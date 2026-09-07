package i18n

// spanishOAuth is the agent consent screen in Spanish.
var spanishOAuth = map[string]string{
	"oauth.title": "Conectar un agente",

	"oauth.asking":            "%[1]s quiere conectarse a tu cuenta de Khepri.",
	"oauth.destination":       "Enviará tu acceso a %[1]s.",
	"oauth.destination.local": "Enviará tu acceso a este ordenador.",

	"oauth.account.signed-in": "Sesión iniciada como %[1]s",
	"oauth.account.notyou":    "¿No eres tú?",

	"oauth.can.title":         "Lo que podrá hacer",
	"oauth.can.read":          "Leer tus objetivos, registros diarios, entrenamiento, datos recordados y documentos.",
	"oauth.can.write":         "Registrar check-ins, completar hábitos, anotar tu peso, registrar comidas y editar tu plan de entrenamiento.",
	"oauth.can.readonly.note": "No podrá cambiar nada.",

	"oauth.readonly.label": "Solo lectura",
	"oauth.readonly.hint":  "Déjalo leer tus datos sin cambiar nada.",

	"oauth.approve":     "Aprobar",
	"oauth.deny":        "Cancelar",
	"oauth.revoke.note": "Puedes revocarlo cuando quieras en Ajustes → Conexiones.",

	"oauth.new.title":    "Crea tu cuenta de Khepri",
	"oauth.new.desc":     "Estás a punto de darle a un agente una memoria que dura. Necesita dónde guardarla.",
	"oauth.new.name":     "Tu nombre",
	"oauth.new.email":    "Correo electrónico",
	"oauth.new.password": "Contraseña",
	"oauth.new.submit":   "Crear cuenta y continuar",
	"oauth.new.haveone":  "¿Ya tienes cuenta?",

	"oauth.signin.title":  "Inicia sesión para continuar",
	"oauth.signin.submit": "Iniciar sesión",
	"oauth.signin.new":    "¿Necesitas una cuenta?",

	"oauth.or":      "o",
	"oauth.google":  "Continuar con Google",
	"oauth.passkey": "Continuar con una passkey",

	"oauth.error.title":          "Esa solicitud no se puede completar",
	"oauth.error.unknown-client": "La aplicación que hace esta solicitud no está registrada en Khepri.",
	"oauth.error.bad-redirect":   "Esta solicitud pedía enviar tu acceso a un sitio que la aplicación no ha registrado.",
	"oauth.error.no-client":      "Esta solicitud no indicó qué aplicación la hace.",
	"oauth.error.expired":        "Esta solicitud ha caducado. Vuelve a empezar desde tu agente.",
	"oauth.error.home":           "Ir a Khepri",
}
