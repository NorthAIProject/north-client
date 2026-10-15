package i18n

// englishNutrition is the food log, the ingredient catalogue and the meal-plan
// builder. Page titles reuse the nav. keys of the same pages.
//
// The macro names come twice: capitalised for a heading or a bar ("Protein"),
// and as they read inside a line ("protein 20 g left"). German would capitalise
// both; the languages here do not, and a lower-cased heading looks broken.
var englishNutrition = map[string]string{
	"nutrition.mono":     "Nutrition",
	"nutrition.add":      "Add",
	"nutrition.remove":   "Remove",
	"nutrition.delete":   "Delete",
	"nutrition.cancel":   "Cancel",
	"nutrition.grams":    "grams",
	"nutrition.days.one": "1 day",
	// %[1]d is a count of days, always more than one.
	"nutrition.days.many": "%[1]d days",

	"nutrition.log.title":            "Today's log",
	"nutrition.log.import":           "Import a meal plan",
	"nutrition.log.ingredient.title": "Log an ingredient",
	"nutrition.log.meal.title":       "Log a meal",
	"nutrition.log.meal.choose":      "Choose a meal",
	"nutrition.log.submit":           "Log",
	"nutrition.log.today":            "Logged today",
	"nutrition.ingredient.choose":    "Choose an ingredient",

	"nutrition.ingredients.intro":              "Shared ingredients plus anything you've added yourself.",
	"nutrition.ingredients.add.title":          "Add an ingredient",
	"nutrition.ingredients.add.per100":         "Per 100g.",
	"nutrition.ingredients.add.submit":         "Add ingredient",
	"nutrition.ingredients.search.title":       "Search",
	"nutrition.ingredients.search.placeholder": "Search ingredients…",
	"nutrition.ingredients.none":               "No ingredients found.",
	"nutrition.ingredients.shared":             "Shared",

	"nutrition.field.name":        "Name",
	"nutrition.field.brand":       "Brand (optional)",
	"nutrition.field.category":    "Category",
	"nutrition.field.calories":    "Calories / 100g",
	"nutrition.field.serving":     "Serving size (g)",
	"nutrition.field.protein":     "Protein (g)",
	"nutrition.field.fat":         "Fat (g)",
	"nutrition.field.carbs":       "Carbs (g)",
	"nutrition.field.description": "Description (optional)",
	"nutrition.field.mode":        "Mode",
	"nutrition.field.daycount":    "Days, starting Monday",
	"nutrition.field.weekdays":    "Weekdays",
	"nutrition.field.custompct":   "Share of your carb target (%)",

	"nutrition.category.protein":   "Protein",
	"nutrition.category.carb":      "Carb",
	"nutrition.category.fat":       "Fat",
	"nutrition.category.dairy":     "Dairy",
	"nutrition.category.vegetable": "Vegetable",
	"nutrition.category.fruit":     "Fruit",
	"nutrition.category.beverage":  "Beverage",
	"nutrition.category.snack":     "Snack",
	"nutrition.category.other":     "Other",

	"nutrition.macro.protein":       "Protein",
	"nutrition.macro.carbs":         "Carbs",
	"nutrition.macro.fat":           "Fat",
	"nutrition.macro.protein.lower": "protein",
	"nutrition.macro.carbs.lower":   "carbs",
	"nutrition.macro.fat.lower":     "fat",
	// %[1]s is a macro as it reads in a line, %[2]s whole grams. The live
	// preview fills both in the browser, so they are strings, not numbers.
	"nutrition.macro.left": "%[1]s %[2]s g left",
	"nutrition.macro.over": "%[1]s %[2]s g over",
	// Eaten / target, then what is left or over.
	"nutrition.macro.summary.left": "%[1].0f / %[2].0f g · %[3].0f g left",
	"nutrition.macro.summary.over": "%[1].0f / %[2].0f g · %[3].0f g over",
	"nutrition.remaining":          "This day: %[1]s",
	"nutrition.preview.after":      "After this: %[1]s",

	"nutrition.plans.import": "Import from file",
	"nutrition.plans.new":    "New plan",
	"nutrition.plans.yours":  "Your plans",
	"nutrition.plans.create": "Create plan",

	"nutrition.target.missing.title":  "Work out your macro target first",
	"nutrition.target.missing.before": "Meal plans are built on the daily target from the",
	"nutrition.target.missing.link":   "calculator",
	"nutrition.target.label":          "Your daily target",
	"nutrition.target.line":           "%[1].0f kcal · %[2].0f g protein · %[3].0f g carbs · %[4].0f g fat",

	"nutrition.mode.easy":          "Easy",
	"nutrition.mode.advanced":      "Advanced",
	"nutrition.mode.easy.desc":     "Easy — pick a carb level and a number of days",
	"nutrition.mode.advanced.desc": "Advanced — custom carbs, chosen weekdays, a different target per day",

	"nutrition.type.no_carb":   "No carb",
	"nutrition.type.low_carb":  "Low carb",
	"nutrition.type.mid_carb":  "Mid carb",
	"nutrition.type.high_carb": "High carb",
	"nutrition.type.custom":    "Custom",
	// %[1].0f is a percentage; %% is a literal percent sign.
	"nutrition.type.custom.pct":    "Custom · %[1].0f%% of carbs",
	"nutrition.type.custom.choice": "Custom share of your carbs",
	// A preset carb level, then its range as a share of the carb target.
	"nutrition.band":       "%[1]s · %[2].0f–%[3].0f%% of your carbs",
	"nutrition.band.grams": " · about %[1].0f g a day",

	"nutrition.daytype.no_carb":   "No carb day",
	"nutrition.daytype.low_carb":  "Low carb day",
	"nutrition.daytype.mid_carb":  "Mid carb day",
	"nutrition.daytype.high_carb": "High carb day",

	"nutrition.override.carbs":   "%[1].0f g carbs",
	"nutrition.override.protein": "%[1].0f g protein",
	"nutrition.override.fat":     "%[1].0f g fat",

	"nutrition.detail.back":   "← All plans",
	"nutrition.detail.import": "Import another plan",
	"nutrition.detail.delete": "Delete plan",

	"nutrition.overage.title":   "That would go over your target",
	"nutrition.overage.confirm": "Save anyway",
	"nutrition.overage.easy":    "Easy plans stay within your target. Choose a smaller portion, remove something, or switch the plan to advanced.",
	// A macro in a line, grams over, grams eaten, grams allowed.
	"nutrition.overage.part": "%[1]s %[2].0f g over (%[3].0f of %[4].0f g)",

	"nutrition.settings.title":         "Plan settings",
	"nutrition.settings.save":          "Save settings",
	"nutrition.settings.advanced.desc": "Advanced mode lets each day have its own carbs, protein and fat, and lets you go over once you confirm.",
	"nutrition.settings.toadvanced":    "Switch to advanced",
	"nutrition.settings.toeasy":        "Switch to easy",
	"nutrition.settings.toeasy.title":  "Switch to easy?",
	"nutrition.settings.toeasy.desc":   "Every day goes back to the plan's default target, and its own carbs, protein and fat are dropped. A day already over that target has to be trimmed first.",
	"nutrition.settings.keepadvanced":  "Keep advanced",

	"nutrition.meal.add":             "Add meal",
	"nutrition.meal.add.placeholder": "Add a meal, e.g. Breakfast",
	"nutrition.meal.remove":          "Remove meal",
	"nutrition.meal.remove.all":      "Remove meal (all options)",

	"nutrition.option.numbered":          "Option %[1]d",
	"nutrition.option.counted":           "Counted",
	"nutrition.option.counts":            "%[1].0f kcal · counts toward the day",
	"nutrition.option.instead":           "%[1].0f kcal · instead of option 1, not counted",
	"nutrition.option.remove":            "Remove option",
	"nutrition.option.add":               "Add option",
	"nutrition.option.label.placeholder": "Option label (optional)",

	"nutrition.portion.estimated":    "estimated",
	"nutrition.portion.optional":     "optional · not counted",
	"nutrition.portion.count":        "Count it",
	"nutrition.portion.makeoptional": "Make optional",

	"nutrition.notes.title": "From your plan",
	"nutrition.notes.desc":  "Advice and recipes from the file you imported. Show them.",

	"nutrition.day.change":      "Change day",
	"nutrition.day.change.desc": "Give this day its own target. Blank fields follow the plan; nothing can go above your daily target.",
	"nutrition.day.followplan":  "Follow the plan",
	"nutrition.day.carbgrams":   "Or exact carb grams",
	"nutrition.day.save":        "Save day",
	"nutrition.day.upto":        "up to %[1].0f",
	"nutrition.day.remove":      "Remove day",
	// %[1]s is a weekday as it reads mid-sentence.
	"nutrition.day.remove.title":    "Remove %[1]s?",
	"nutrition.day.remove.one":      "Its meal goes with it.",
	"nutrition.day.remove.many":     "Its %[1]d meals go with it.",
	"nutrition.day.keep":            "Keep it",
	"nutrition.day.choose":          "Choose a weekday",
	"nutrition.day.add":             "Add day",
	"nutrition.day.add.named":       "Add %[1]s",
	"nutrition.meal.optional.badge": "optional · not counted",
	"nutrition.meal.count":          "Count meal",
	"nutrition.meal.make.optional":  "Make meal optional",
	"nutrition.option.optional":     "%[1].0f kcal · optional meal, not counted",
}
