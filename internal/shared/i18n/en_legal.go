package i18n

// englishLegal is the privacy policy, the terms, and the Facebook data page.
//
// One paragraph per key, inline markup included: emphasis and links have to
// move with the words around them, which a sentence split at every tag cannot
// do in another language. web/legal renders these unescaped (see prose there),
// so keep the markup to <strong>, <em>, <code>, <a> and nothing that runs.
//
// The English is the binding text. A change of substance here is a change to
// the policy: bump legal.LastUpdated and update the other three languages in
// the same commit.
//
// Verbs in the arguments: %[1]s and up are the operator details from
// web/legal/legal.go, escaped before they arrive.
var englishLegal = map[string]string{
	"legal.updated":         "Last updated %[1]s",
	"legal.doc.translation": "This is a translation. The English text is the version that applies; if the two differ, the English prevails.",
	"legal.foot.privacy":    "Privacy",
	"legal.foot.terms":      "Terms",
	"legal.foot.home":       "Home",

	// Privacy policy.
	"legal.privacy.page":       "Privacy",
	"legal.privacy.title":      "Privacy policy",
	"legal.privacy.lead":       "Khepri holds an unusual amount of information about the people who use it — what they weigh, how they slept, where they ran, and what they wrote in a journal at eleven at night. This page says exactly what is stored, where it goes, and how to get it back or delete it.",
	"legal.privacy.controller": "%[1]s (%[2]s) is the controller of that data. Questions, requests and complaints: <a href=\"mailto:%[3]s\">%[3]s</a>.",

	"legal.privacy.stored.h":            "What is stored",
	"legal.privacy.stored.p":            "Only what you or an integration you connected put there. Nothing is bought from a data broker and nothing is inferred from third-party tracking.",
	"legal.privacy.stored.account":      "<strong>Account</strong> — email address, display name, a password hash (never the password), timezone, coaching-tone preference. If you sign in with Google or a passkey, the identifier that provider gives us.",
	"legal.privacy.stored.health":       "<strong>Health and body data</strong> — weight, body measurements, sleep, hydration, and any biometric readings you record or a connected device pushes.",
	"legal.privacy.stored.activity":     "<strong>Activity and location</strong> — workouts, training plans, activity sessions, and, if you connect Strava, your activities including the route traces they carry. A route trace is location data about where you actually were.",
	"legal.privacy.stored.nutrition":    "<strong>Nutrition</strong> — foods, meal plans and what you logged.",
	"legal.privacy.stored.writing":      "<strong>Your writing</strong> — check-ins, journal entries, decisions, goals, and every message you exchange with the coach.",
	"legal.privacy.stored.memory":       "<strong>Memory</strong> — durable facts the system derives from your conversations so it does not have to ask twice. You can see, edit, pin and delete these.",
	"legal.privacy.stored.documents":    "<strong>Documents</strong> — files you upload, the text extracted from them, and numeric embeddings of that text used for search.",
	"legal.privacy.stored.integrations": "<strong>Integrations</strong> — access tokens for services you connect, encrypted at rest; your Telegram chat identifier if you link it; tokens issued to agents you authorise.",
	"legal.privacy.stored.operational":  "<strong>Operational records</strong> — sign-in events, account activity, model usage and cost, and error logs.",

	"legal.privacy.consent.h":  "Health data needs your explicit consent",
	"legal.privacy.consent.p1": "Most of the list above is ordinary personal data. Weight, sleep, biometrics and anything health-related in your journal are <em>special category</em> data under Article 9 of the UK and EU GDPR, and are processed <strong>only on your explicit consent</strong>, given when you create an account and record it.",
	"legal.privacy.consent.p2": "You can withdraw that consent at any time by deleting the data or your account. Withdrawal does not undo processing that already happened, and Khepri stops working in any meaningful way without it — a coach with no health data is a chat window.",

	"legal.privacy.where.h":                  "Where your data goes",
	"legal.privacy.where.p":                  "Khepri is not advertising-funded and sells nothing to anyone. Data reaches other companies only where the product cannot work otherwise:",
	"legal.privacy.where.ai":                 "<strong>AI providers.</strong> This is the disclosure that matters most. To answer you, the coach sends the model your message together with the context it assembled — which can include your goals, recent conversations, check-ins, training and nutrition summaries, and passages from your own documents. Which company receives it depends on the provider configured for your account, and you may supply your own API key instead, in which case your data goes to your provider under your own agreement with them.",
	"legal.privacy.where.strava":             "<strong>Strava</strong> — only if you connect it, and only to read your activities.",
	"legal.privacy.where.telegram":           "<strong>Telegram</strong> — only if you link a chat. Your messages and the coach's replies pass through Telegram's servers, under Telegram's own policy.",
	"legal.privacy.where.analytics.events":   "<strong>Product analytics.</strong> A small number of events — account created, onboarding finished, a source connected, the coach answered — recorded against your account identifier to see whether the product works. No health data, no message content, no document text.",
	"legal.privacy.where.analytics.pages":    "Your browser also reports the pages you open. Inside the application that is the address only, with anything after the <code>?</code> removed, so a page's contents and any link that carries a token are not included. On the public pages — the home page, sign-up, sign-in, these policies, and the screen that asks whether to connect an agent — it also reports which buttons and links get clicked, which is how we tell a page that reads well from one that does not.",
	"legal.privacy.where.analytics.sessions": "The home page and the connect-an-agent screen additionally record the session itself, so we can see where a first-time visitor gets stuck. Everything typed into a field is masked before it leaves the browser, and no page you reach after signing in is ever recorded.",
	"legal.privacy.where.hosting":            "<strong>Hosting, storage and email</strong> — the servers the application runs on, object storage for files you upload, and a mail provider for account email such as password resets.",

	"legal.privacy.kept.h":  "How long it is kept",
	"legal.privacy.kept.p1": "Until you delete it. Long conversations are summarised rather than discarded, because the point of the product is that it remembers — so a thread you had months ago may survive as a summary after its individual messages are compacted.",
	"legal.privacy.kept.p2": "Deleting your account removes your data. Backups and logs containing incidental records may persist for a short period before rotating out.",

	"legal.privacy.rights.h":      "What you can do",
	"legal.privacy.rights.p":      "Under the UK and EU GDPR you may access, correct, delete, export, restrict or object to the processing of your data, and withdraw consent. Two of these need no request at all:",
	"legal.privacy.rights.export": "<strong>Export</strong> — Settings → a complete archive of your data, downloaded immediately.",
	"legal.privacy.rights.delete": "<strong>Deletion</strong> — Settings → delete account.",
	"legal.privacy.rights.other":  "For anything else, write to <a href=\"mailto:%[1]s\">%[1]s</a>. If you are unhappy with the response you may complain to your data protection authority — in the UK, the Information Commissioner's Office.",

	"legal.privacy.security.h":  "Security, honestly stated",
	"legal.privacy.security.p1": "Passwords are hashed. Integration tokens are encrypted at rest. Sessions are protected against cross-site request forgery, and you can see your account's sign-in history.",
	"legal.privacy.security.p2": "Khepri is built and run by a very small team. It is not independently audited and holds no security certification. If that is not an acceptable custodian for your health data, the honest advice is not to store it here.",

	"legal.privacy.changes.h": "Changes",
	"legal.privacy.changes.p": "Material changes will be announced in the application before they take effect. The date at the top of this page is when it last changed.",

	// Terms of service.
	"legal.terms.page":  "Terms",
	"legal.terms.title": "Terms of service",
	"legal.terms.lead":  "The agreement between you and %[1]s, covering your use of Khepri. Plain terms, and one section that matters more than the rest: Khepri is not a doctor.",

	"legal.terms.medical.h":      "Not medical advice",
	"legal.terms.medical.p1":     "Khepri is a personal coaching and journalling tool. It is not a medical device, and nothing it produces — training plans, calorie targets, observations about your sleep, or anything the coach says — is medical, nutritional or psychological advice.",
	"legal.terms.medical.p2":     "Its answers are generated by language models. They can be confidently wrong, and they can be wrong about you specifically. Do not use Khepri to diagnose or treat anything, do not stop or start a treatment because of something it said, and speak to a qualified professional before making a meaningful change to how you eat, train or medicate.",
	"legal.terms.medical.crisis": "<strong>If you are in crisis or may be in danger, contact your local emergency services or a crisis line. Khepri is not monitored by a human and cannot help you.</strong>",

	"legal.terms.account.h": "Your account",
	"legal.terms.account.p": "You must be at least 16, provide accurate details, keep your credentials to yourself, and remain responsible for what happens under your account. One account is for one person.",

	"legal.terms.use.h":       "Acceptable use",
	"legal.terms.use.intro":   "Do not:",
	"legal.terms.use.rights":  "upload anything you do not have the right to upload;",
	"legal.terms.use.others":  "upload another person's health data without their consent;",
	"legal.terms.use.probe":   "attempt to reach another account's data, or to break, overload or probe the service;",
	"legal.terms.use.resell":  "resell the service, or use it to build a competing dataset;",
	"legal.terms.use.illegal": "use it for anything illegal.",
	"legal.terms.use.suspend": "Accounts that do these things can be suspended without notice.",

	"legal.terms.content.h":  "Your content stays yours",
	"legal.terms.content.p1": "You keep every right you have in what you write and upload. You grant %[1]s only the permission needed to run the service for you: to store your content, process it, send the relevant parts to the AI provider serving your account, and show it back to you.",
	"legal.terms.content.p2": "Your content is not used to train models. Note that the AI provider receiving it has its own terms, and if you supply your own API key those terms are between you and that provider.",

	"legal.terms.avail.h":  "Availability, and what you should assume",
	"legal.terms.avail.p1": "Khepri is early software offered as it is, without warranty of any kind. There is no uptime commitment. Features may change or be removed. It may lose data.",
	"legal.terms.avail.p2": "<strong>Keep your own copy of anything you cannot afford to lose.</strong> Settings → Export produces a complete archive at any time, and using it periodically is the sensible assumption to operate under.",

	"legal.terms.paid.h": "Paid plans",
	"legal.terms.paid.p": "Khepri is currently free to use. If paid plans are introduced, the terms, prices and any effect on existing accounts will be published before they apply, and you will not be charged without agreeing first.",

	"legal.terms.ending.h": "Ending it",
	"legal.terms.ending.p": "You may delete your account at any time from Settings, which removes your data as described in the <a href=\"/privacy\">privacy policy</a>. %[1]s may suspend or close an account that breaches these terms, or discontinue the service with reasonable notice and an opportunity to export.",

	"legal.terms.liability.h":  "Liability",
	"legal.terms.liability.p1": "To the fullest extent the law allows, %[1]s is not liable for indirect or consequential loss, lost data, or lost profits, and total liability for any claim is limited to the greater of the amount you paid in the preceding twelve months or £50.",
	"legal.terms.liability.p2": "Nothing here limits liability for death or personal injury caused by negligence, for fraud, or for anything else that cannot lawfully be limited. If you are a consumer, your statutory rights are unaffected.",

	"legal.terms.law.h": "Governing law",
	"legal.terms.law.p": "These terms are governed by the laws of %[1]s, whose courts have exclusive jurisdiction — without depriving a consumer of the protection of their home country's mandatory law.",

	"legal.terms.contact.h": "Contact",

	// Facebook data deletion instructions.
	"legal.fb.page":              "Facebook data",
	"legal.fb.title":             "Your Facebook data in Khepri",
	"legal.fb.lead":              "Khepri uses Facebook only to find friends of yours who are already here. You choose to connect it from Friends, and you can take it away again at any time.",
	"legal.fb.keeps.h":           "What Khepri keeps",
	"legal.fb.keeps.p1":          "Your Facebook account's app-scoped ID, linked to your Khepri account, so friends who connect later can find you. Nothing else: not your name or photo from Facebook, not your friends list, and not the access token Facebook hands over while you connect. That token is used once to read which of your friends use Khepri and is then thrown away.",
	"legal.fb.keeps.p2":          "The Khepri accounts that a connection found are kept for one hour so you can follow them, then deleted.",
	"legal.fb.delete.h":          "How to delete it",
	"legal.fb.delete.disconnect": "Sign in to Khepri, open <a href=\"/app/friends\">Friends</a>, and choose <strong>Disconnect Facebook</strong>. The link and any results are deleted at once.",
	"legal.fb.delete.account":    "Or delete your Khepri account under <a href=\"/app/settings\">Settings</a>, which deletes everything, including the Facebook link.",
	"legal.fb.delete.facebook":   "You can also remove Khepri from Facebook itself, under Settings &amp; privacy → Settings → Apps and websites. Khepri then cannot read anything new from Facebook; disconnect in Khepri as well to delete the link it holds.",
	"legal.fb.delete.email":      "If you cannot sign in, write to %[1]s from the address on your account and we will delete it for you.",
}
