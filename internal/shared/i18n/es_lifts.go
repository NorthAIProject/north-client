package i18n

var spanishLifts = map[string]string{
	"lifts.today.title":  "Registrar una serie",
	"lifts.today.import": "Importar desde Hevy",
	"lifts.today.desc":   "Cada serie alimenta tus marcas, el mapa muscular y lo que sabe el entrenador.",

	"lifts.form.exercise":    "Ejercicio",
	"lifts.form.exercise.ph": "Press de banca",
	"lifts.form.kg":          "kg",
	"lifts.form.reps":        "Repeticiones",
	"lifts.form.rir":         "En reserva",
	"lifts.form.rir.help":    "En reserva: las reps que te quedaban. 0 es al fallo; déjalo vacío si no lo sabes.",
	"lifts.form.kind":        "Tipo de serie",
	"lifts.form.submit":      "Registrar serie",
	"lifts.kind.work":        "Efectiva",
	"lifts.kind.warmup":      "Calentamiento",
	"lifts.kind.drop":        "Serie descendente",
	"lifts.list.aria":        "Series de hoy",
	"lifts.list.drop":        "Descendente",
	"lifts.list.remove":      "Quitar %[1]s %[2]s",

	"lifts.import.page":  "Importar desde Hevy",
	"lifts.import.label": "Importar",
	"lifts.import.title": "Trae tu historial de Hevy",
	"lifts.import.lede":  "Cada entreno se convierte aquí en una sesión, con sus series, calentamientos y esfuerzo, para que tus marcas, el mapa muscular y el entrenador partan de lo que ya has hecho.",
	"lifts.import.step1": "En Hevy: Perfil → Ajustes → Exportar e importar datos → Exportar entrenamientos.",
	"lifts.import.step2": "Guarda el CSV y suéltalo aquí. Hasta 10 MB.",
	"lifts.import.step3": "Si importas el mismo archivo otra vez, no se duplica nada.",
	"lifts.import.back":  "Volver al entrenamiento",
	"lifts.import.drop":  "Suelta tu CSV de Hevy o búscalo",
	"lifts.import.busy":  "Importando tus entrenos…",

	"lifts.result.already":    "Ya estaba importado",
	"lifts.result.imported":   "Importados %[1]d entrenos, %[2]d series",
	"lifts.result.duplicates": "%[1]d entrenos ya estaban aquí y se han dejado como estaban.",
	"lifts.result.skipped":    "%[1]d filas no eran series de fuerza (cardio, isométricos por tiempo o números imposibles) y se han omitido.",
	"lifts.result.unmatched":  "Estos ejercicios no están en el catálogo, así que cuentan para marcas y volumen, pero no para el mapa muscular:",
}
