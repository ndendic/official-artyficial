module herdr-console

go 1.27.1

require (
	github.com/nats-io/nats.go v1.52.0
	github.com/ndendic/data-start v0.0.0-00010101000000-000000000000
)

require (
	github.com/klauspost/compress v1.20.0 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

// The RT Station Go SDK is not public yet: building needs a data-start
// checkout beside this repository (../../../data-start from this folder),
// or DATA_START_DIR set to one (see build.go).
replace github.com/ndendic/data-start => ../../../data-start
