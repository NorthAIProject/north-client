package i18n

// englishConnections is the agent connections page: MCP keys, the Telegram
// link, the bring-your-own-provider card, and the calendar server.
//
// The page the topbar pill points at, so it is the first thing a person sees
// after taking the prompt.
var englishConnections = map[string]string{
	"conn.intro": "Let an agent you already use — Claude Code, Codex, Hermes — read your goals, log check-ins, and ask your coach. Each connection gets its own key, so you can turn one off without touching the others.",
	"conn.back":  "Back to account settings",

	"conn.issued.ready":       "%[1]s is ready",
	"conn.issued.desc":        "Copy this now. It is not stored, so it cannot be shown again — if you lose it, revoke this connection and make another.",
	"conn.issued.key":         "Key",
	"conn.issued.env":         "Environment",
	"conn.issued.prompt.note": "Or paste this into the agent itself and let it do the setup. It sends the key to whichever model that agent runs on, so use it with an agent you would trust with your coaching data — and revoke the key here if you change your mind.",
	"conn.issued.prompt":      "Setup prompt",

	"conn.list.title":   "Connected agents",
	"conn.list.desc":    "Every one of these can read and write your coaching data. Revoke anything you do not recognise.",
	"conn.list.empty":   "Nothing connected yet.",
	"conn.list.waiting": "waiting for first use…",

	"conn.revoke":       "Revoke",
	"conn.revoke.title": "Revoke %[1]s?",
	"conn.revoke.desc":  "That agent stops being able to read or change anything straight away. The key cannot be turned back on; you would connect the agent again with a new one.",
	"conn.keep":         "Keep it",

	"conn.tg.title":            "Telegram",
	"conn.tg.desc":             "Message your coach from your phone. Same memory, same goals, same conversation — what you ask in Telegram carries on in the web chat, and the other way round.",
	"conn.tg.tap":              "Tap the button. Telegram opens the bot and sends the code for you. It works once, and only for the next 15 minutes.",
	"conn.tg.open":             "Open in Telegram",
	"conn.tg.linked":           "Linked %[1]s",
	"conn.tg.code":             "Get a link code",
	"conn.tg.openbot":          "Open",
	"conn.tg.manual":           "Open the Khepri bot in Telegram and start a chat. The bot name is not configured on this server, so ask the person who set it up.",
	"conn.tg.disconnect":       "Disconnect",
	"conn.tg.disconnect.title": "Disconnect Telegram?",
	"conn.tg.disconnect.desc":  "The bot stops answering that chat straight away. Nothing you have already said is deleted — the conversation stays in the web app. To connect again you would get a new code.",

	"conn.ai.title":      "Model provider",
	"conn.ai.desc":       "By default Khepri supplies the AI. Point this account at your own key — or at your own Hermes gateway — if you have one.",
	"conn.ai.noenc":      "Not available on this server — it has no encryption key configured, and Khepri will not store a provider key it cannot encrypt.",
	"conn.ai.notools":    "This provider answers, but it does not call Khepri's tools — so it cannot log a session, edit your plan, or read the exercise catalogue. Khepri's own model handles those turns instead. Everything else still uses your provider.",
	"conn.ai.free":       "Nothing to set up. Free accounts are served by models that cost nothing to run.",
	"conn.ai.pays":       "Your account pays for the tokens, and Khepri stops paying for yours.",
	"conn.ai.provider":   "Provider",
	"conn.ai.gateway":    "Gateway URL",
	"conn.ai.instance":   "Each account runs its own instance — tailnet, LAN, or public HTTPS.",
	"conn.ai.apikey":     "API key",
	"conn.ai.stored":     "A key ending %[1]s is stored. Leave this blank to keep it.",
	"conn.ai.encrypted":  "Khepri stores this encrypted and can never show it back to you.",
	"conn.ai.model":      "Model",
	"conn.ai.model.desc": "Leave blank for this provider's default. Free text rather than a list, because a hardcoded list of models goes stale within weeks.",
	"conn.ai.save":       "Save provider",
	"conn.ai.claude":     "Want Claude? Use an OpenRouter key and a model beginning",
	"conn.ai.remove":     "Remove my key",

	"conn.connect.title":      "Connect an agent",
	"conn.connect.desc":       "Pick the agent you use. This shows what you will paste; nothing is created until you ask for a key.",
	"conn.connect.this":       "This Khepri",
	"conn.connect.selfhosted": "Self-hosted",
	"conn.connect.nojs":       "Every client's setup, since the tabs above need JavaScript to switch.",
	"conn.connect.handoff":    "Or hand this to the agent and let it configure itself.",
	"conn.connect.create":     "Create a key for %[1]s",

	"conn.name":      "Name this connection",
	"conn.name.ph":   "Laptop",
	"conn.name.desc": "Name it after the machine it will live on, so you know which one to revoke later.",
	"conn.hosted":    "Prefer the hosted surface unless you are the only person using this Khepri. Its keys are per person and revocable without a restart.",

	"conn.cal.title":         "Calendar",
	"conn.cal.desc":          "Connect a calendar MCP server and your coach can see the next seven days — so it stops suggesting a Thursday session when you are already booked.",
	"conn.cal.disconnect":    "Disconnect",
	"conn.cal.endpoint":      "Server address",
	"conn.cal.endpoint.desc": "The MCP endpoint of a server that can list your calendar events.",
	"conn.cal.token":         "Access token",
	"conn.cal.token.ph":      "Leave empty if the server needs no token",
	"conn.cal.token.desc":    "Stored encrypted. It is never shown again after you save it.",
	"conn.cal.connect":       "Connect calendar",
}
