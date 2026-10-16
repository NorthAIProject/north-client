package i18n

// spanishOutreach is the coach speaking first: its message title, the answers under
// it, what each answer did, and the fallback when the coach could not write.
var spanishOutreach = map[string]string{
	"outreach.title":                    "De tu coach",
	"outreach.action.undo":              "Deshacer",
	"outreach.action.lighter":           "Hacerla más suave",
	"outreach.action.keep":              "Mantener el plan",
	"outreach.action.today":             "Hacerla hoy",
	"outreach.action.tomorrow":          "Mañana",
	"outreach.action.let_go":            "Dejarla pasar",
	"outreach.done.undo":                "Deshecho: hoy vuelves al plan.",
	"outreach.done.lighter":             "Hecho: hoy es más suave, cerca del 60% de las series.",
	"outreach.done.keep":                "Mantenido: hoy va según el plan.",
	"outreach.done.today":               "Movida: está para hoy.",
	"outreach.done.tomorrow":            "Movida: está para mañana.",
	"outreach.done.let_go":              "Dejada pasar: el plan la retoma la próxima vez.",
	"outreach.fallback.lighter.applied": "Tu recuperación está por debajo de lo habitual esta mañana, así que he reducido %s a cerca del 60% de las series. Deshazlo si prefieres entrenar según el plan.",
	"outreach.fallback.lighter.offer":   "Tu recuperación está por debajo de lo habitual esta mañana. ¿Hacemos %s más suave hoy, cerca del 60% de las series?",
	"outreach.fallback.missed":          "%s no se hizo ayer. ¿La movemos o la dejamos pasar?",
	"outreach.error.action":             "Esa respuesta no está disponible para este mensaje.",
	"outreach.error.answered":           "Eso ya se respondió.",
	"outreach.error":                    "No se pudo hacer ahora. Inténtalo desde la app.",
}
