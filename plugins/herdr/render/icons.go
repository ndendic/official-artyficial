package render

// Icon is an agent's mark: Claude, Codex and Pi get their own, every other
// kind the generic terminal. Each is one colour (currentColor), so the
// shell sets it per kind through .hc-ico[data-kind] and it works in both
// themes. The shapes are simple marks drawn for the console, not the
// projects' official logos.
func Icon(kind string) string { return icon(kind, "hc-ico") }

// PanelIcon is the same mark for a row of the Agents panel (.ha-ico).
func PanelIcon(kind string) string { return icon(kind, "ha-ico") }

func icon(kind, class string) string {
	k := kind
	var paths string
	switch kind {
	case "claude":
		paths = `<g fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round"><path d="M12 3v18M3 12h18M5.6 5.6l12.8 12.8M18.4 5.6 5.6 18.4"/></g>`
	case "codex":
		paths = `<g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2.8 20 7.4v9.2L12 21.2 4 16.6V7.4z"/><path d="m8.5 10 2.5 2-2.5 2M12.5 14.5h3"/></g>`
	case "pi":
		paths = `<g fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M4.5 7.5h15M9 7.5v11M15.5 7.5v8.5c0 1.6.8 2.5 2.4 2.5"/></g>`
	case "more":
		paths = `<g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="6" cy="12" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="18" cy="12" r="1.2"/></g>`
	default:
		k = "other"
		paths = `<g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4.5" width="18" height="15" rx="3"/><path d="m7.5 10 2.5 2-2.5 2M12.5 14.5h4"/></g>`
	}
	return `<span class="` + class + `" data-kind="` + k + `" aria-hidden="true"><svg viewBox="0 0 24 24" width="18" height="18">` + paths + `</svg></span>`
}
