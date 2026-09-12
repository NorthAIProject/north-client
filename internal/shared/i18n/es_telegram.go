package i18n

var spanishTelegram = map[string]string{
	"tg.start":             "Estás conectado a %[1]s. Pregúntame lo que preguntarías en la app web — tengo la misma memoria, los mismos objetivos y los mismos registros.\n\nEnvía /help para ver qué puedo hacer.",
	"tg.help":              "Pregúntame lo que preguntarías en la app web — cómo va un objetivo, cómo ha sido tu semana, si entrenar hoy.\n\nEsta es la misma conversación que el chat de la web. Pregunta aquí, y la respuesta también está allí.\n\nAntes de escribir nada — un registro, un objetivo — te enseño lo que voy a hacer y espero un sí.\n\n/help — este mensaje\n/stats — cómo van las cosas (añade week, month o year)\n/unlink — desconectar este chat de tu cuenta",
	"tg.notlinked":         "Este chat no está conectado a ninguna cuenta de Khepri.",
	"tg.stats.unavailable": "Todavía no puedo leer tus números en este servidor. Están todos en la aplicación web.",
	"tg.stats.failed":      "No he podido leer tus números ahora mismo. Inténtalo dentro de un momento.",
	"tg.stats.empty":       "Aún no hay nada registrado en ese periodo.",
	"tg.unlinked":          "Desconectado. Nada de lo que has dicho se borra — la conversación entera sigue en la app web. Para volver a conectar, consigue un código nuevo en Ajustes → Conexiones de agentes.",

	"tg.onboard":     "Termina de configurar tu cuenta en la app web de Khepri primero, y luego vuelve a escribirme.",
	"tg.linked":      "Conectado a %[1]s. Escríbeme cuando quieras — tengo la misma memoria y los mismos objetivos que la app web.",
	"tg.takenlink":   "Este chat ya está conectado a otra cuenta de Khepri.",
	"tg.toomany":     "Demasiados intentos. Espera un minuto y prueba tu código otra vez.",
	"tg.photofailed": "No se ha podido guardar esa foto.",

	"tg.voice.unavailable": "Todavía no puedo escuchar notas de voz en este servidor. Escríbelo y te respondo igual.",
	"tg.voice.toolong":     "Esa nota de voz es más larga de lo que puedo escuchar. Prueba con una más corta, o escríbelo.",
	"tg.voice.toobig":      "Esa grabación es más grande de lo que puedo procesar. Prueba con una más corta.",
	"tg.voice.failed":      "No he podido entender esa nota de voz. Inténtalo otra vez, o escríbelo.",
	"tg.voice.silent":      "No he oído nada ahí. ¿Lo intentas otra vez?",
	"tg.voice.download":    "No he podido descargar esa nota de voz. ¿Me la envías otra vez?",
	"tg.wrong":             "Algo ha ido mal por mi lado. Inténtalo de nuevo en un momento.",

	"tg.confirm.again": "Todavía necesito un sí o un no primero.",
	"tg.confirm":       "Antes de hacer esto, ¿me lo confirmas?",
	"tg.confirm.yes":   "Sí, hazlo",
	"tg.confirm.no":    "No",

	"tg.quota.minute": "Has llegado a tu límite de mensajes con el entrenador. Inténtalo de nuevo en menos de un minuto.",
	"tg.quota.hour":   "Has llegado a tu límite de mensajes con el entrenador. Inténtalo de nuevo dentro de una hora aproximadamente.",
	"tg.quota.n":      "Has llegado a tu límite de mensajes con el entrenador. Inténtalo de nuevo en %[1]d minutos.",
}
