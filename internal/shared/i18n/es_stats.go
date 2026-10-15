package i18n

var spanishStats = map[string]string{
	"stats.pattern.sleep_late_caffeine.title.up":   "Duermes %[1]s más después de tomar cafeína tarde",
	"stats.pattern.sleep_late_caffeine.title.down": "Duermes %[1]s menos después de tomar cafeína tarde",
	"stats.pattern.sleep_late_caffeine.detail":     "%[1]s en %[2]d noches tras tomar cafeína pasada tu hora límite, %[3]s en las otras %[4]d noches.",
	"stats.pattern.sleep_late_caffeine.a":          "Tras cafeína pasada tu hora límite",
	"stats.pattern.sleep_late_caffeine.b":          "Otras noches",

	"stats.pattern.sleep_late_eating.title.up":   "Duermes %[1]s más después de comer tarde",
	"stats.pattern.sleep_late_eating.title.down": "Duermes %[1]s menos después de comer tarde",
	"stats.pattern.sleep_late_eating.detail":     "%[1]s en %[2]d noches tras comer después de cerrar la cocina, %[3]s en %[4]d noches con una última comida más temprana.",
	"stats.pattern.sleep_late_eating.a":          "Tras comer tarde",
	"stats.pattern.sleep_late_eating.b":          "Tras una última comida más temprana",

	"stats.pattern.energy_sleep.title.up":   "Tu energía es %[1]s puntos más alta después de dormir 7 horas o más",
	"stats.pattern.energy_sleep.title.down": "Tu energía es %[1]s puntos más baja después de dormir 7 horas o más",
	"stats.pattern.energy_sleep.detail":     "La energía de tus registros promedia %[1]s en %[2]d días tras 7 horas o más, y %[3]s en %[4]d días tras menos.",
	"stats.pattern.energy_sleep.a":          "Tras 7 horas o más",
	"stats.pattern.energy_sleep.b":          "Tras menos de 7 horas",

	"stats.pattern.mood_training.title.up":   "Tu ánimo es %[1]s puntos más alto los días que entrenas",
	"stats.pattern.mood_training.title.down": "Tu ánimo es %[1]s puntos más bajo los días que entrenas",
	"stats.pattern.mood_training.detail":     "El ánimo de tus registros promedia %[1]s en %[2]d días de entrenamiento y %[3]s en %[4]d días de descanso.",
	"stats.pattern.mood_training.a":          "Días de entrenamiento",
	"stats.pattern.mood_training.b":          "Días de descanso",

	"stats.pattern.mood_sleep.title.up":   "Tu ánimo es %[1]s puntos más alto después de dormir 7 horas o más",
	"stats.pattern.mood_sleep.title.down": "Tu ánimo es %[1]s puntos más bajo después de dormir 7 horas o más",
	"stats.pattern.mood_sleep.detail":     "El ánimo de tus registros promedia %[1]s en %[2]d días tras 7 horas o más, y %[3]s en %[4]d días tras menos.",
	"stats.pattern.mood_sleep.a":          "Tras 7 horas o más",
	"stats.pattern.mood_sleep.b":          "Tras menos de 7 horas",

	"stats.pattern.mood_outdoor.title.up":   "Tu ánimo es %[1]s puntos más alto los días que entrenas al aire libre",
	"stats.pattern.mood_outdoor.title.down": "Tu ánimo es %[1]s puntos más bajo los días que entrenas al aire libre",
	"stats.pattern.mood_outdoor.detail":     "El ánimo de tus registros promedia %[1]s en %[2]d días con un entrenamiento al aire libre y %[3]s en los otros %[4]d días.",
	"stats.pattern.mood_outdoor.a":          "Días que entrenaste al aire libre",
	"stats.pattern.mood_outdoor.b":          "Otros días",

	"stats.pattern.hrv_training.title.up":   "Tu VFC es %[1]s ms más alta el día después de entrenar",
	"stats.pattern.hrv_training.title.down": "Tu VFC es %[1]s ms más baja el día después de entrenar",
	"stats.pattern.hrv_training.detail":     "Tu VFC promedia %[1]s ms en %[2]d días después de entrenar y %[3]s ms en %[4]d días después de descansar.",
	"stats.pattern.hrv_training.a":          "El día después de entrenar",
	"stats.pattern.hrv_training.b":          "El día después de descansar",

	"stats.pattern.rhr_training.title.up":   "Tu frecuencia cardíaca en reposo es %[1]s bpm más alta el día después de entrenar",
	"stats.pattern.rhr_training.title.down": "Tu frecuencia cardíaca en reposo es %[1]s bpm más baja el día después de entrenar",
	"stats.pattern.rhr_training.detail":     "Tu frecuencia cardíaca en reposo promedia %[1]s bpm en %[2]d días después de entrenar y %[3]s bpm en %[4]d días después de descansar.",
	"stats.pattern.rhr_training.a":          "El día después de entrenar",
	"stats.pattern.rhr_training.b":          "El día después de descansar",

	"stats.pattern.energy_training.title.up":   "Tu energía es %[1]s puntos más alta los días que entrenas",
	"stats.pattern.energy_training.title.down": "Tu energía es %[1]s puntos más baja los días que entrenas",
	"stats.pattern.energy_training.detail":     "La energía de tus registros promedia %[1]s en %[2]d días de entrenamiento y %[3]s en %[4]d días de descanso.",
	"stats.pattern.energy_training.a":          "Días de entrenamiento",
	"stats.pattern.energy_training.b":          "Días de descanso",

	"stats.pattern.sleep_screen.title.up":   "Duermes %[1]s más después de días con más de %[2]s de pantalla",
	"stats.pattern.sleep_screen.title.down": "Duermes %[1]s menos después de días con más de %[2]s de pantalla",
	"stats.pattern.sleep_screen.detail":     "%[1]s en %[2]d noches tras días de más pantalla, %[3]s en %[4]d noches tras días más ligeros.",
	"stats.pattern.sleep_screen.a":          "Tras días de mucha pantalla",
	"stats.pattern.sleep_screen.b":          "Tras días más ligeros",

	"stats.pattern.mood_measure.title.up":   "Tu ánimo es %[1]s puntos más alto los días con %[2]s",
	"stats.pattern.mood_measure.title.down": "Tu ánimo es %[1]s puntos más bajo los días con %[2]s",
	"stats.pattern.mood_measure.detail":     "El ánimo de tus registros promedia %[1]s en %[2]d días con %[5]s y %[3]s en los otros %[4]d días.",
	"stats.pattern.mood_measure.a":          "Días con %[1]s",
	"stats.pattern.mood_measure.b":          "Otros días",

	"stats.pattern.mood_steps.what": "%[1]s pasos o más",

	"stats.pattern.mood_daylight.what": "%[1]s o más a la luz del día",

	"stats.pattern.mood_stand.what": "%[1]s horas de pie o más",

	"stats.pattern.mood_mindful.title.up":   "Tu ánimo es %[1]s puntos más alto los días que te tomas un momento de mindfulness",
	"stats.pattern.mood_mindful.title.down": "Tu ánimo es %[1]s puntos más bajo los días que te tomas un momento de mindfulness",
	"stats.pattern.mood_mindful.detail":     "El ánimo de tus registros promedia %[1]s en %[2]d días con una sesión de mindfulness y %[3]s en %[4]d días sin ella.",
	"stats.pattern.mood_mindful.a":          "Días con una sesión de mindfulness",
	"stats.pattern.mood_mindful.b":          "Días sin ella",

	"stats.best.fastest_5k": "5 km más rápidos",

	"stats.best.fastest_10k": "10 km más rápidos",

	"stats.best.longest_run": "Carrera más larga",

	"stats.best.longest_ride": "Salida en bici más larga",

	"stats.best.fastest_20k": "Salida en bici más rápida de más de 20 km",

	"stats.best.most_climb": "Mayor desnivel",

	"stats.best.longest_walk": "Caminata más larga",

	"stats.best.longest_swim": "Sesión de natación más larga",

	"stats.best.longest_session": "Sesión más larga",

	"stats.kind.running": "Correr",

	"stats.kind.cycling": "Ciclismo",

	"stats.kind.walking": "Caminar",

	"stats.kind.swimming": "Natación",

	"stats.kind.indoor_running": "Correr en interior",

	"stats.kind.indoor_cycling": "Ciclismo en interior",
}
