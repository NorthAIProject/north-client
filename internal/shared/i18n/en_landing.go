package i18n

// englishLanding is the marketing page: hero, the argument sections, and
// pricing.
//
// The 3D-model attribution notice in the footer is deliberately absent. Like
// the legal pages, a licence attribution is a statement of obligation rather
// than copy — CC BY-SA requires the notice, and a translated one is a different
// notice.
var englishLanding = map[string]string{
	"land.waypoint.day1":   "Day 1",
	"land.waypoint.month8": "Month 8",
	"land.waypoint.year2":  "Year 2",

	"land.hero.badge": "An operating system for personal growth",
	"land.hero.h1a":   "Most AI forgets you by Tuesday.",
	"land.hero.h1b":   "Khepri has been keeping notes since March.",
	"land.hero.lede":  "One coach with one memory — on the web, in Telegram, and inside any tool that speaks MCP. It holds your goals, your training, the photos you send, your documents, and every check-in you have ever written.",
	"land.hero.see":   "See it work",
	"land.hero.free":  "Free while you find out whether it sticks. No card.",

	"land.film.label":    "Product film · 4s",
	"land.film.desc":     "One memory across surfaces — coaching thread, goals, training anatomy, check-ins.",
	"land.film.autoplay": "Autoplay · silent loop",
	"land.film.aria":     "Khepri product film: persistent coaching memory across web and messaging surfaces",

	"land.thread.one":  "One thread",
	"land.thread.span": "Mar — Aug",

	// The hero conversation. Five months in one thread, four surfaces, nothing
	// repeated back — which is the whole claim, made as a picture rather than a
	// sentence.
	"land.thread.1.date": "Mar 14",
	"land.thread.1.body": "Starting over. Bad back, three days a week, and I want to hike Peneda-Gerês in September.",
	"land.thread.2.date": "Mar 14",
	"land.thread.2.body": "Then we build around the back first. Six weeks of hinge tolerance before anything gets heavy.",
	"land.thread.3.date": "Apr 02",
	"land.thread.3.body": "Deadlifted 60kg today. Nothing hurt.",
	"land.thread.4.date": "May 19",
	"land.thread.4.body": "Fourth session in a row with no pain. That is the signal I was waiting for — moving you into the loading block.",
	"land.thread.5.date": "Aug 03",
	"land.thread.5.body": "Gerês is five weeks out. Your longest carry so far is 40 minutes. We should get that to 90.",

	"land.feat.eyebrow": "What you get",
	"land.feat.title":   "Built to still be useful in month eight.",
	"land.feat.1.name":  "Persistent memory",
	"land.feat.1.body":  "Every goal, session, check-in, and offhand remark stays. Khepri reads back across months, not across the last ten messages, so the injury you mentioned in March still shapes the plan in September.",
	"land.feat.2.name":  "Context-aware coaching",
	"land.feat.2.body":  "Before it answers, Khepri assembles what it knows: active goals, this week's training, your documents, your calendar, how you have been sleeping. Advice arrives already fitted to your life.",
	"land.feat.3.name":  "Everywhere, and inside your tools",
	"land.feat.3.body":  "Web and Telegram share one memory. Send a photo from either. Through MCP, Khepri can also act — reading your notes, filing a check-in, pulling a summary — from whatever agent you already use.",
	"land.feat.4.name":  "Fitness intelligence",
	"land.feat.4.body":  "Strava flows in as summaries rather than raw activity dumps. Plans respect the equipment you actually own. Send a form clip or a photo and you get cues, not a lecture.",
	"land.feat.5.name":  "Knowledge you already have",
	"land.feat.5.body":  "Drop in the research, the PDFs, the physio notes. Khepri searches them when they are relevant and cites what it used, instead of pretending to remember.",

	"land.how.eyebrow": "How it works",
	"land.how.title":   "Four things happen.",
	"land.how.1.when":  "Minute one",
	"land.how.1.name":  "Say where you are going",
	"land.how.1.body":  "In your own words, badly. Khepri turns it into an objective, a horizon, and a first week.",
	"land.how.2.when":  "Week one",
	"land.how.2.name":  "It builds the context",
	"land.how.2.body":  "Your equipment, your limits, your calendar, your documents. The plan is assembled from what is true about you, not from a template.",
	"land.how.3.when":  "Every week after",
	"land.how.3.name":  "You just live",
	"land.how.3.body":  "Check in from whichever app is in your hand. A sentence is enough. Khepri is keeping the ledger.",
	"land.how.4.when":  "Month three",
	"land.how.4.name":  "It changes the plan first",
	"land.how.4.body":  "When the pattern shifts — sleep, pain, a missed block — Khepri moves before you fall off, and tells you what it saw.",

	"land.vision.eyebrow": "Why we are building it",
	"land.vision.quote":   "The useful thing was never the answer. It was that somebody had been watching long enough to notice the pattern.",
	"land.vision.body":    "A good coach is not smarter than the internet. A good coach knows that you always quit in week five, that you lie about sleep, and that the last time you tried this you injured yourself doing something you were told not to do. That knowledge takes months to build and it is the entire product. Khepri is not trying to be a better chatbot — it is trying to be the thing that has been paying attention.",
	"land.vision.1.value": "Six months",
	"land.vision.1.body":  "The horizon plans are written against, not the session in front of you.",
	"land.vision.2.value": "Two surfaces",
	"land.vision.2.body":  "Web and Telegram — and one memory behind both.",
	"land.vision.3.value": "Zero",
	"land.vision.3.body":  "Times you re-explain your injury, your equipment, or what you are training for.",

	"land.price.eyebrow":     "Pricing",
	"land.price.title":       "One plan today. The other when it is real.",
	"land.price.free":        "Free",
	"land.price.free.amount": "€0",
	"land.price.free.period": "today",
	"land.price.free.blurb":  "The whole product. Nothing is held back behind a plan that does not exist yet.",
	"land.price.free.1":      "The coach on the web and on Telegram, one memory behind both",
	"land.price.free.2":      "Training plans, with looping illustrations on most movements",
	"land.price.free.3":      "Form checks — upload a clip, get cues back",
	"land.price.free.4":      "Voice notes on the web, transcribed and filed",
	"land.price.free.5":      "Strava, Google Calendar, and your own PDFs",
	"land.price.free.6":      "MCP access from your own agents and editors",
	"land.price.free.cta":    "Start free",
	"land.price.pro":         "Pro",
	"land.price.pro.amount":  "€15",
	"land.price.pro.period":  "per month, eventually",
	"land.price.pro.blurb":   "Not available yet, and nothing about your account is waiting on it. When running Khepri starts costing real money, this is where the ceilings go — not the features.",
	"land.price.pro.1":       "Everything in Free, still there",
	"land.price.pro.2":       "Higher limits on form checks and document knowledge",
	"land.price.pro.3":       "The stronger model chain for planning and review",
	"land.price.pro.badge":   "Coming soon",
	"land.price.pro.nothing": "Nothing to do here yet",
	"land.price.footnote":    "There is nothing to pay for and nothing to cancel. Your memory is exportable, and it is yours.",

	"land.cta.title":   "In six months it will know things about you that you have forgotten.",
	"land.cta.body":    "That only works if it starts now. The first conversation is the shortest one you will ever have with it.",
	"land.cta.signin":  "Sign in",
	"land.cta.create":  "Create your account",
	"land.cta.open":    "Open Khepri",
	"land.nav.started": "Get started",

	"land.footer.tagline": "Khepri — an operating system for personal growth",
	"land.footer.privacy": "Privacy",
	"land.footer.terms":   "Terms",
	"land.footer.source":  "Source",

	"legal.english": "This page is available in English only. The English text is the version that applies.",
}
