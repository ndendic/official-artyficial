// Command herdr-console is the Plugin artyficial.herdr: drive herdr
// sessions (a terminal multiplexer for coding agents) from the pill of an
// RT Station Workspace.
//
// It is a Broker Tier Plugin whose Subject Declaration asks for Caller
// Tickets and nothing else on the delivery prefix:
//
//	"subjects": {"publish": [], "subscribe": [], "request": ["artyficial.herdr.console"], "caller": true}
//
// The pill posts actions to the one capability, artyficial.herdr.console.
// Each call brings a fresh Caller Ticket of the browser session that made
// it, and the Plugin answers by patching that session (core.ui.caller.patch
// through sdk/go/publisher.Caller): the picker, the new-session form and
// the thread into the Patch Region console, the session chip through the
// signal pillAgent, and the patch blocks agents write.
//
// The workspace invokes the capability through the stdio frame (protocol
// 2), where the ticket is WireRequest.Caller.Ticket. The Plugin also serves
// the capability on the Broker (a micro endpoint on its request Subject),
// where the ticket is the header Data-Start-Caller-Ticket (the name core
// gives it).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"

	"github.com/ndendic/data-start/api/envelope"
	"github.com/ndendic/data-start/api/manifest"
	"github.com/ndendic/data-start/sdk/go/publisher"

	"herdr-console/console"
	"herdr-console/herdr"
	"herdr-console/render"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("herdr-console: ")
	cfg, err := loadConfig(pluginEnv("CONFIG_FILE"))
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}
	url, service, inbox := pluginEnv(strings.TrimPrefix(manifest.BrokerURLEnv, legacyPrefix)), pluginEnv(strings.TrimPrefix(manifest.ServiceNameEnv, legacyPrefix)), pluginEnv(strings.TrimPrefix(manifest.InboxPrefixEnv, legacyPrefix))
	if url == "" || service == "" || inbox == "" {
		log.Fatal("no Broker Credential in the environment: enable the package as a Broker Tier Plugin")
	}
	// Leave the connection Name empty: the Auth Callout reads a nonempty
	// Name as a session Attestation. Never log the URL: it holds the
	// credential. nats.go reconnects when the Authorization Lease (30 s)
	// ends the connection, and is authorized again.
	nc, err := natsgo.Connect(url,
		natsgo.CustomInboxPrefix(inbox),
		natsgo.Timeout(5*time.Second),
		natsgo.MaxReconnects(-1),
		natsgo.ReconnectWait(250*time.Millisecond),
		natsgo.ErrorHandler(func(_ *natsgo.Conn, _ *natsgo.Subscription, err error) {
			log.Printf("Broker: %s", strings.ReplaceAll(fmt.Sprint(err), url, "<broker>"))
		}),
	)
	if err != nil {
		log.Fatalf("dial the Broker: %v", strings.ReplaceAll(err.Error(), url, "<broker>"))
	}
	defer nc.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cli := &herdr.CLI{Bin: cfg.HerdrBin, Socket: cfg.HerdrSocket}
	watch := herdr.NewWatcher(cli, cfg.HerdrSocket)
	go watch.Run(ctx)
	home := herdr.Home()
	con := console.New(ctx, console.Config{
		Crew:             cfg.Crew,
		ConsoleWorkspace: cfg.ConsoleWorkspace,
		ClaudeProjects:   filepath.Join(home, ".claude", "projects"),
		CodexSessions:    filepath.Join(home, ".codex", "sessions"),
		PiSessions:       filepath.Join(home, ".pi", "agent", "sessions"),
		DataDir:          pluginEnv("DATA_DIR"),
	}, cli, watch, func(ticket string) console.Patcher { return publisher.NewCaller(nc, ticket) })
	defer con.Close()
	render.HomeDir = home

	h := handler(con.Handle)
	if err := serveBroker(ctx, nc, service, h); err != nil {
		log.Printf("the Broker endpoint is off (the stdio frame still serves): %v", err)
	}
	log.Printf("ready: herdr %s, socket %s", cli.Bin, cfg.HerdrSocket)
	serve(ctx, os.Stdin, os.Stdout, h)
	stop()
}

// legacyPrefix is the previous release's name of the Plugin variables; core
// exports every variable under both names for one release.
const legacyPrefix = "DATA_START_PLUGIN_"

// pluginEnv reads one variable core sets for a managed Plugin: RT_PLUGIN_<name>
// first, then the previous release's DATA_START_PLUGIN_<name>, so this Plugin
// starts under either binary.
func pluginEnv(name string) string {
	if v := os.Getenv("RT_PLUGIN_" + name); v != "" {
		return v
	}
	return os.Getenv(legacyPrefix + name)
}

// serveBroker adds the micro endpoint on the request Subject. The ticket
// is the header the workspace sets; a request without one patches nothing.
func serveBroker(ctx context.Context, nc *natsgo.Conn, service string, h handler) error {
	svc, err := micro.AddService(nc, micro.Config{Name: service, Version: consoleVersion, QueueGroupDisabled: true})
	if err != nil {
		return err
	}
	return svc.AddEndpoint("console", micro.HandlerFunc(func(r micro.Request) {
		ticket := r.Headers().Get("Data-Start-Caller-Ticket")
		cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		result, fault := invoke(cctx, h, consoleID, consoleVersion, r.Data(), ticket)
		reply := envelope.Reply{OK: fault == nil, Result: result, Fault: fault}
		raw, _ := json.Marshal(reply)
		_ = r.Respond(raw)
	}),
		micro.WithEndpointSubject(consoleID),
		micro.WithEndpointMetadata(map[string]string{"descriptor_id": consoleID, "descriptor_version": consoleVersion}),
		micro.WithEndpointQueueGroupDisabled())
}

// serve is the stdio loop of protocol 2: one JSON object per line. Each
// request runs in its own goroutine, so a slow herdr call never holds the
// next one; answers are written whole, one per line.
func serve(ctx context.Context, in io.Reader, out io.Writer, h handler) {
	var mu sync.Mutex
	enc := json.NewEncoder(out)
	write := func(r manifest.WireResponse) {
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(r)
	}
	write(manifest.WireResponse{Ready: true})
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), manifest.MaxMessageBytes)
	var wg sync.WaitGroup
	defer wg.Wait()
	for scan.Scan() {
		line := append([]byte(nil), scan.Bytes()...)
		var req manifest.WireRequest
		if json.Unmarshal(line, &req) == nil && req.Shutdown {
			write(manifest.WireResponse{ID: req.ID})
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			write(answer(cctx, line, h))
		}()
	}
}
