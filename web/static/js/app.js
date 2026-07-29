// Global front-end helpers: toast notifications, theme toggle, fetch wrapper
// with CSRF handling, and Alpine.js stores. Loaded on every page via base.html.
//
// Kept dependency-free on purpose — the whole SPA already relies on Alpine from
// a CDN and a bundler would mean rewriting half the templates.

(function () {
	"use strict";

	// ---------- CSRF ----------
	function csrfToken() {
		const el = document.querySelector('meta[name="csrf-token"]');
		return el ? el.getAttribute("content") : "";
	}
	function csrfHeaders(extra) {
		return Object.assign({ "X-CSRF-Token": csrfToken() }, extra || {});
	}
	window.csrfToken = csrfToken;
	window.csrfHeaders = csrfHeaders;

	// ---------- Fetch wrapper ----------
	// Thin wrapper over fetch that adds CSRF, parses JSON, and surfaces errors
	// as thrown Errors with a .status field so callers can switch on it.
	async function api(path, opts) {
		opts = opts || {};
		const method = (opts.method || "GET").toUpperCase();
		const headers = Object.assign({}, opts.headers || {});
		if (method !== "GET" && method !== "HEAD") {
			headers["X-CSRF-Token"] = csrfToken();
		}
		if (opts.json !== undefined) {
			headers["Content-Type"] = "application/json";
			opts.body = JSON.stringify(opts.json);
			delete opts.json;
		}
		const resp = await fetch(path, Object.assign({}, opts, { headers }));
		const ct = resp.headers.get("Content-Type") || "";
		const body = ct.includes("application/json") ? await resp.json().catch(() => null) : await resp.text();
		if (!resp.ok) {
			const msg = (body && body.error) || (typeof body === "string" && body) || `Request failed (${resp.status})`;
			const err = new Error(msg);
			err.status = resp.status;
			err.body = body;
			throw err;
		}
		return body;
	}
	window.api = api;

	// ---------- Toast ----------
	// Minimal toast manager. Toasts auto-dismiss after 4s by default; errors
	// stay until dismissed so users can read what went wrong.
	const ToastContainer = {
		el: null,
		ensure() {
			if (this.el) return;
			this.el = document.createElement("div");
			this.el.id = "toasts";
			this.el.setAttribute("aria-live", "polite");
			this.el.setAttribute("role", "status");
			this.el.className = "fixed top-4 right-4 z-[100] flex flex-col gap-2 pointer-events-none";
			document.body.appendChild(this.el);
		},
	};

	function toast(opts) {
		if (typeof opts === "string") opts = { message: opts };
		const kind = opts.kind || "info";
		const sticky = kind === "error" || opts.sticky === true;
		ToastContainer.ensure();

		const wrapper = document.createElement("div");
		wrapper.className = [
			"pointer-events-auto min-w-[240px] max-w-sm rounded-lg shadow-lg px-4 py-3 text-sm flex items-start gap-3",
			"transition transform duration-200 translate-x-4 opacity-0",
			kindClasses(kind),
		].join(" ");
		wrapper.setAttribute("role", kind === "error" ? "alert" : "status");

		const iconHTML = kindIcon(kind);
		wrapper.innerHTML = `
			<div class="shrink-0 mt-0.5">${iconHTML}</div>
			<div class="flex-1 leading-snug"></div>
			<button type="button" class="ml-2 opacity-60 hover:opacity-100 shrink-0" aria-label="Dismiss">✕</button>
		`;
		wrapper.querySelector("div.flex-1").textContent = opts.message || "";
		const dismiss = () => {
			wrapper.classList.add("translate-x-4", "opacity-0");
			setTimeout(() => wrapper.remove(), 200);
		};
		wrapper.querySelector("button").addEventListener("click", dismiss);

		ToastContainer.el.appendChild(wrapper);
		requestAnimationFrame(() => wrapper.classList.remove("translate-x-4", "opacity-0"));
		if (!sticky) setTimeout(dismiss, opts.duration || 4000);
		return dismiss;
	}
	function kindClasses(kind) {
		switch (kind) {
			case "success": return "bg-green-600 text-white";
			case "error":   return "bg-red-600 text-white";
			case "warning": return "bg-amber-500 text-white";
			default:        return "bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900";
		}
	}
	function kindIcon(kind) {
		const common = 'width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"';
		switch (kind) {
			case "success": return `<svg ${common}><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>`;
			case "error":   return `<svg ${common}><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/></svg>`;
			case "warning": return `<svg ${common}><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4m0 4h.01M4.93 19h14.14A2 2 0 0021 17.07V6.93A2 2 0 0019.07 5H4.93A2 2 0 003 6.93v10.14A2 2 0 004.93 19z"/></svg>`;
			default:        return `<svg ${common}><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>`;
		}
	}
	toast.success = (m, o) => toast(Object.assign({ kind: "success", message: m }, o));
	toast.error   = (m, o) => toast(Object.assign({ kind: "error",   message: m }, o));
	toast.warning = (m, o) => toast(Object.assign({ kind: "warning", message: m }, o));
	toast.info    = (m, o) => toast(Object.assign({ kind: "info",    message: m }, o));

	// toast.fromError takes a thrown error from window.api() and renders a
	// user-friendly message instead of the raw HTTP string. Prefers a short
	// server-provided error field when present, then falls back to status-
	// code templates, then to "we can't reach the server."
	toast.fromError = function (err, opts) {
		const status = err && err.status;
		const body = err && err.body;
		const raw = (err && err.message) || String(err || "Something went wrong.");

		// Server-supplied field wins if it's short enough to be intentional.
		if (body && typeof body.error === "string" && body.error && body.error.length < 200) {
			return toast(Object.assign({ kind: "error", message: body.error }, opts || {}));
		}

		if (!status) {
			return toast(Object.assign({
				kind: "error",
				message: "Can't reach the server — check your connection and try again.",
			}, opts || {}));
		}

		let message;
		switch (true) {
			case status === 400: message = "Something in that request didn't look right."; break;
			case status === 401: message = "You need to sign in again for this."; break;
			case status === 403: message = "You don't have permission to do that."; break;
			case status === 404: message = "We couldn't find that."; break;
			case status === 409: message = "That conflicts with something that already exists."; break;
			case status === 413: message = "That file is too big."; break;
			case status === 415: message = "That file type isn't supported."; break;
			case status === 429: message = "You're going a bit fast — give it a moment."; break;
			case status === 502 || status === 503 || status === 504:
				message = "An upstream service is having trouble. Try again shortly."; break;
			case status >= 500:
				message = "Something broke on our side. We've logged it — please try again."; break;
			default:
				message = raw;
		}
		return toast(Object.assign({ kind: "error", message }, opts || {}));
	};
	window.toast = toast;

	// ---------- URL sanitising ----------
	//
	// Alpine's :href does not sanitise anything, and Go's html/template never
	// sees these values — they arrive as JSON and are bound in the browser. So
	// a `javascript:` URL from a third-party feed becomes a live script that
	// runs on click, which our CSP cannot stop (it allows 'unsafe-inline').
	//
	// Everything we bind that we did not construct ourselves goes through
	// this: OpenStreetMap tags (anyone can edit them, and /around-me is a
	// public page), scraped listing URLs, and Wikidata sitelinks. Data we do
	// build server-side is already constrained — trip journal media, for one,
	// is prefix-checked before storage — but binding it here too costs nothing
	// and removes the need to know which is which at every call site.
	//
	// Returns null rather than "" for a rejected value: Alpine drops an
	// attribute bound to null, so the anchor renders as plain text instead of
	// a link that silently goes nowhere.
	function safeUrl(raw) {
		if (typeof raw !== "string") return null;
		const candidate = raw.trim();
		if (candidate === "") return null;
		// Root-relative paths are ours by construction. Reject "//host" —
		// that is protocol-relative and points off-origin.
		if (candidate.startsWith("/") && !candidate.startsWith("//")) return candidate;
		try {
			const parsed = new URL(candidate, window.location.origin);
			if (parsed.protocol === "http:" || parsed.protocol === "https:") {
				return parsed.href;
			}
		} catch (err) {
			// Unparseable is not linkable.
		}
		return null;
	}
	window.safeUrl = safeUrl;

	// ---------- Theme ----------
	// Applies the theme as early as possible so there's no FOUC. The inline
	// script in base.html <head> handles the first paint; this manager owns
	// toggling at runtime.
	const THEME_KEY = "re:theme"; // "light" | "dark" | "system"
	function resolveTheme(pref) {
		if (pref === "dark" || pref === "light") return pref;
		return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
	}
	function applyTheme(pref) {
		const resolved = resolveTheme(pref);
		document.documentElement.classList.toggle("dark", resolved === "dark");
		document.documentElement.setAttribute("data-theme", resolved);
	}
	function getThemePref() {
		return localStorage.getItem(THEME_KEY) || "system";
	}
	function setThemePref(pref) {
		if (pref === "system") localStorage.removeItem(THEME_KEY);
		else localStorage.setItem(THEME_KEY, pref);
		applyTheme(pref);
	}
	window.theme = {
		get: getThemePref,
		set: setThemePref,
		toggle() {
			const current = resolveTheme(getThemePref());
			setThemePref(current === "dark" ? "light" : "dark");
		},
	};
	// Listen for system preference changes when the user is in "system" mode.
	try {
		window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
			if (getThemePref() === "system") applyTheme("system");
		});
	} catch (_) {}

	// ---------- Alpine store registration ----------
	document.addEventListener("alpine:init", () => {
		// Compare drawer store — tracks listings the user wants to compare.
		Alpine.store("compare", {
			items: JSON.parse(localStorage.getItem("re:compare") || "[]"),
			open: false,
			max: 4,
			has(id) { return this.items.some((x) => x.id === id); },
			add(listing) {
				if (this.has(listing.id)) return;
				if (this.items.length >= this.max) {
					toast.warning(`You can compare up to ${this.max} stays at once.`);
					return;
				}
				this.items.push(listing);
				this.persist();
				toast.success(`Added to compare (${this.items.length}/${this.max})`);
			},
			remove(id) {
				this.items = this.items.filter((x) => x.id !== id);
				this.persist();
			},
			clear() {
				this.items = [];
				this.persist();
				this.open = false;
			},
			persist() {
				localStorage.setItem("re:compare", JSON.stringify(this.items));
			},
		});

		// Theme store exposed to templates for the toggle UI.
		Alpine.store("theme", {
			pref: getThemePref(),
			init() {
				this.pref = getThemePref();
			},
			set(p) {
				this.pref = p;
				setThemePref(p);
			},
			toggle() {
				window.theme.toggle();
				this.pref = getThemePref();
			},
		});
	});

	// ---------- Keyboard shortcuts ----------
	document.addEventListener("keydown", (e) => {
		const tag = (e.target && e.target.tagName) || "";
		const typing = tag === "INPUT" || tag === "TEXTAREA" || (e.target && e.target.isContentEditable);

		// "/" focuses primary search (if page declares one with data-search-input).
		if (!typing && e.key === "/" && !e.metaKey && !e.ctrlKey) {
			const input = document.querySelector("[data-search-input]");
			if (input) {
				e.preventDefault();
				input.focus();
			}
		}
		// Cmd/Ctrl-K opens the command palette (a future home for quick nav).
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
			e.preventDefault();
			window.dispatchEvent(new CustomEvent("re:open-palette"));
		}
		// Shift-? opens the shortcut help overlay.
		if (!typing && e.shiftKey && e.key === "?") {
			e.preventDefault();
			window.dispatchEvent(new CustomEvent("re:open-shortcuts"));
		}
	});

	// ---------- Command palette ----------
	// Registered globally so templates can use `x-data="commandPalette()"`.
	// Static commands cover primary navigation; a destination search pings
	// /api/v1/destinations so users can jump straight to a place they typed.
	const staticCommands = [
		{ id: "nav-explore",   kind: "go", title: "Explore destinations", subtitle: "/explore",   url: "/explore",   icon: iconCompass() },
		{ id: "nav-listings",  kind: "go", title: "Browse stays",         subtitle: "/listings",  url: "/listings",  icon: iconHome() },
		{ id: "nav-flights",   kind: "go", title: "Search flights",       subtitle: "/flights",   url: "/flights",   icon: iconPlane() },
		{ id: "nav-scrape",    kind: "go", title: "Scrape rentals",       subtitle: "/scrape",    url: "/scrape",    icon: iconSearch() },
		{ id: "nav-dashboard", kind: "go", title: "Go to dashboard",      subtitle: "/dashboard", url: "/dashboard", icon: iconHome() },
		{ id: "nav-trips",     kind: "go", title: "My trips",             subtitle: "/trips",     url: "/trips",     icon: iconMap() },
		{ id: "nav-watchlist", kind: "go", title: "Watchlist",            subtitle: "/watchlist", url: "/watchlist", icon: iconHeart() },
		{ id: "nav-profile",   kind: "go", title: "Profile settings",     subtitle: "/profile",   url: "/profile",   icon: iconUser() },
		{ id: "act-theme",     kind: "act", title: "Toggle theme",        subtitle: "Switch light/dark",
		  action: () => window.theme.toggle(), icon: iconMoon() },
		{ id: "act-compare",   kind: "act", title: "Clear compare list",  subtitle: "Remove all selected stays",
		  action: () => { Alpine.store("compare").clear(); }, icon: iconCompare() },
		{ id: "act-shortcuts", kind: "act", title: "Keyboard shortcuts",  subtitle: "Show the cheatsheet",
		  action: () => window.dispatchEvent(new CustomEvent("re:open-shortcuts")), icon: iconKey() },
	];

	// Recent-use memory for the palette — persists across sessions so the
	// top commands float to the top next time.
	const PALETTE_RECENT_KEY = "re:palette-recent";
	function recordPaletteUse(id) {
		try {
			const prior = JSON.parse(localStorage.getItem(PALETTE_RECENT_KEY) || "[]");
			const filtered = prior.filter((r) => r.id !== id);
			filtered.unshift({ id, at: Date.now() });
			localStorage.setItem(PALETTE_RECENT_KEY, JSON.stringify(filtered.slice(0, 10)));
		} catch (_) {}
	}
	function recentPaletteIDs() {
		try {
			return JSON.parse(localStorage.getItem(PALETTE_RECENT_KEY) || "[]").map((r) => r.id);
		} catch (_) { return []; }
	}

	window.commandPalette = function () {
		return {
			open: false,
			query: "",
			cursor: 0,
			remote: [],
			timer: null,
			init() {
				window.addEventListener("re:open-palette", () => this.show());
			},
			show() {
				this.open = true;
				this.query = "";
				this.cursor = 0;
				this.remote = [];
				this.$nextTick(() => this.$refs.input && this.$refs.input.focus());
			},
			close() {
				this.open = false;
			},
			get results() {
				const q = this.query.trim().toLowerCase();
				if (!q) {
					// No query → surface recent commands first, then the rest.
					const recent = recentPaletteIDs();
					const byId = Object.fromEntries(staticCommands.map((c) => [c.id, c]));
					const recentItems = recent.map((id) => byId[id]).filter(Boolean);
					const seen = new Set(recent);
					const remainder = staticCommands.filter((c) => !seen.has(c.id));
					return recentItems.concat(remainder, this.remote);
				}
				const base = staticCommands.filter((c) =>
					(c.title + " " + (c.subtitle || "")).toLowerCase().includes(q)
				);
				return base.concat(this.remote);
			},
			move(delta) {
				const n = this.results.length;
				if (n === 0) return;
				this.cursor = (this.cursor + delta + n) % n;
				// Keep the highlighted row inside the scrollable viewport.
				// Without this, arrowing past the bottom leaves the cursor
				// invisible while the list stays pinned.
				this.$nextTick(() => {
					const el = this.$root.querySelector('[role="option"][aria-selected="true"]');
					if (el && el.scrollIntoView) {
						el.scrollIntoView({ block: "nearest", behavior: "auto" });
					}
				});
			},
			run() {
				const item = this.results[this.cursor];
				if (!item) return;
				recordPaletteUse(item.id);
				if (item.kind === "act") {
					item.action && item.action();
					this.close();
					return;
				}
				if (item.url) {
					this.close();
					window.location.href = item.url;
				}
			},
			_debounceSearch() {
				if (this.timer) clearTimeout(this.timer);
				this.timer = setTimeout(() => this.fetchRemote(), 200);
			},
			async fetchRemote() {
				const q = this.query.trim();
				if (q.length < 2) {
					this.remote = [];
					return;
				}
				try {
					const data = await window.api(`/api/v1/destinations?q=${encodeURIComponent(q)}&limit=6`);
					const destinations = (data && data.destinations) || [];
					this.remote = destinations.map((d) => ({
						id: `dest-${d.id || d.name}`,
						kind: "destination",
						title: d.name,
						subtitle: d.country || "",
						url: `/explore/${encodeURIComponent(d.name)}`,
						icon: iconPin(),
					}));
				} catch (_) {
					this.remote = [];
				}
			},
		};
	};

	// Tiny inline SVG helpers used by the palette. Keeping them here rather
	// than a separate asset avoids a layout shift when the palette opens.
	function svg(body) { return `<svg width="18" height="18" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">${body}</svg>`; }
	function iconCompass() { return svg(`<circle cx="12" cy="12" r="9"/><path stroke-linecap="round" stroke-linejoin="round" d="M8 16l2-6 6-2-2 6-6 2z"/>`); }
	function iconHome()    { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"/>`); }
	function iconPlane()   { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8"/>`); }
	function iconSearch()  { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M11 19a8 8 0 100-16 8 8 0 000 16z"/>`); }
	function iconMap()     { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M9 20l-5.447-2.724A1 1 0 013 16.382V5.618a1 1 0 011.447-.894L9 7m0 13l6-3m-6 3V7m6 10l5.553 2.776A1 1 0 0021 18.882V8.118a1 1 0 00-.553-.894L15 4m0 13V4m0 0L9 7"/>`); }
	function iconHeart()   { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M4.318 6.318a4.5 4.5 0 000 6.364L12 20.364l7.682-7.682a4.5 4.5 0 00-6.364-6.364L12 7.636l-1.318-1.318a4.5 4.5 0 00-6.364 0z"/>`); }
	function iconUser()    { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M5.121 17.804A13.937 13.937 0 0112 16c2.5 0 4.847.655 6.879 1.804M15 10a3 3 0 11-6 0 3 3 0 016 0zm6 2a9 9 0 11-18 0 9 9 0 0118 0z"/>`); }
	function iconMoon()    { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"/>`); }
	function iconCompare() { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M8 7h12m0 0l-4-4m4 4l-4 4M16 17H4m0 0l4 4m-4-4l4-4"/>`); }
	function iconKey()     { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M15 7a4 4 0 11-8 0 4 4 0 018 0zm0 0h5l-2 2m0 0l2 2m-2-2v4"/>`); }
	function iconPin()     { return svg(`<path stroke-linecap="round" stroke-linejoin="round" d="M17.657 16.657L13.414 20.9a2 2 0 01-2.827 0l-4.244-4.243a8 8 0 1111.314 0z"/><path stroke-linecap="round" stroke-linejoin="round" d="M15 11a3 3 0 11-6 0 3 3 0 016 0z"/>`); }

	// ---------- Service worker ----------
	if ("serviceWorker" in navigator && location.protocol !== "file:") {
		window.addEventListener("load", () => {
			navigator.serviceWorker.register("/static/service-worker.js").catch((err) => {
				console.warn("Service worker registration failed:", err);
			});
		});
	}
})();
