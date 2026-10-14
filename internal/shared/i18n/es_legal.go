package i18n

var spanishLegal = map[string]string{
	"legal.updated":         "Última actualización: %[1]s",
	"legal.doc.translation": "Esta es una traducción. La versión que se aplica es el texto en inglés; si ambos difieren, prevalece el inglés.",
	"legal.foot.privacy":    "Privacidad",
	"legal.foot.terms":      "Términos",
	"legal.foot.home":       "Inicio",

	"legal.privacy.page":       "Privacidad",
	"legal.privacy.title":      "Política de privacidad",
	"legal.privacy.lead":       "Khepri guarda una cantidad inusual de información sobre quienes lo usan: cuánto pesan, cómo durmieron, por dónde corrieron y lo que escribieron en un diario a las once de la noche. Esta página explica exactamente qué se guarda, adónde va y cómo recuperarlo o eliminarlo.",
	"legal.privacy.controller": "%[1]s (%[2]s) es el responsable del tratamiento de esos datos. Preguntas, solicitudes y reclamaciones: <a href=\"mailto:%[3]s\">%[3]s</a>.",

	"legal.privacy.stored.h":            "Qué se guarda",
	"legal.privacy.stored.p":            "Solo lo que guardas tú o una integración que hayas conectado. No se compra nada a intermediarios de datos y no se deduce nada a partir de rastreo de terceros.",
	"legal.privacy.stored.account":      "<strong>Cuenta</strong>: dirección de correo electrónico, nombre visible, un hash de la contraseña (nunca la contraseña), zona horaria y preferencia de tono del entrenador. Si inicias sesión con Google o con una llave de acceso, el identificador que nos da ese proveedor.",
	"legal.privacy.stored.health":       "<strong>Datos de salud y del cuerpo</strong>: peso, medidas corporales, sueño, hidratación y cualquier lectura biométrica que registres o que envíe un dispositivo conectado.",
	"legal.privacy.stored.activity":     "<strong>Actividad y ubicación</strong>: entrenos, planes de entrenamiento, sesiones de actividad y, si conectas Strava, tus actividades, incluidos los recorridos que contienen. Un recorrido es un dato de ubicación sobre dónde estuviste realmente.",
	"legal.privacy.stored.nutrition":    "<strong>Nutrición</strong>: alimentos, planes de comidas y lo que registraste.",
	"legal.privacy.stored.writing":      "<strong>Lo que escribes</strong>: registros diarios, entradas del diario, decisiones, objetivos y cada mensaje que intercambias con el entrenador.",
	"legal.privacy.stored.memory":       "<strong>Memoria</strong>: hechos duraderos que el sistema extrae de tus conversaciones para no tener que preguntar dos veces. Puedes verlos, editarlos, fijarlos y eliminarlos.",
	"legal.privacy.stored.documents":    "<strong>Documentos</strong>: los archivos que subes, el texto extraído de ellos y representaciones numéricas (embeddings) de ese texto que se usan para buscar.",
	"legal.privacy.stored.integrations": "<strong>Integraciones</strong>: tokens de acceso de los servicios que conectas, cifrados en reposo; tu identificador de chat de Telegram si lo vinculas; tokens emitidos a los agentes que autorizas.",
	"legal.privacy.stored.operational":  "<strong>Registros operativos</strong>: inicios de sesión, actividad de la cuenta, uso y coste de los modelos, y registros de errores.",

	"legal.privacy.consent.h":  "Los datos de salud necesitan tu consentimiento explícito",
	"legal.privacy.consent.p1": "Casi todo lo anterior son datos personales ordinarios. El peso, el sueño, los datos biométricos y cualquier cosa relacionada con la salud en tu diario son datos de <em>categoría especial</em> según el artículo 9 del RGPD del Reino Unido y de la UE, y se tratan <strong>solo con tu consentimiento explícito</strong>, que das al crear una cuenta y al registrarlos.",
	"legal.privacy.consent.p2": "Puedes retirar ese consentimiento en cualquier momento eliminando los datos o tu cuenta. Retirarlo no deshace el tratamiento que ya se hizo, y sin él Khepri deja de funcionar de forma útil: un entrenador sin datos de salud es una ventana de chat.",

	"legal.privacy.where.h":                  "Adónde van tus datos",
	"legal.privacy.where.p":                  "Khepri no se financia con publicidad y no vende nada a nadie. Los datos llegan a otras empresas solo cuando el producto no puede funcionar de otra manera:",
	"legal.privacy.where.ai":                 "<strong>Proveedores de IA.</strong> Esta es la información más importante. Para responderte, el entrenador envía al modelo tu mensaje junto con el contexto que ha reunido, que puede incluir tus objetivos, conversaciones recientes, registros diarios, resúmenes de entrenamiento y nutrición, y fragmentos de tus propios documentos. Qué empresa lo recibe depende del proveedor configurado para tu cuenta. También puedes usar tu propia clave de API; en ese caso, tus datos van a tu proveedor según tu propio acuerdo con él.",
	"legal.privacy.where.strava":             "<strong>Strava</strong>: solo si lo conectas, y solo para leer tus actividades.",
	"legal.privacy.where.telegram":           "<strong>Telegram</strong>: solo si vinculas un chat. Tus mensajes y las respuestas del entrenador pasan por los servidores de Telegram, bajo la política de Telegram.",
	"legal.privacy.where.analytics.events":   "<strong>Analítica del producto.</strong> Un número reducido de eventos (cuenta creada, configuración inicial terminada, fuente conectada, respuesta del entrenador) que se registran con el identificador de tu cuenta para saber si el producto funciona. Ni datos de salud, ni contenido de mensajes, ni texto de documentos.",
	"legal.privacy.where.analytics.pages":    "Tu navegador también informa de las páginas que abres. Dentro de la aplicación, solo la dirección, sin nada de lo que va después del <code>?</code>, de modo que no se incluye ni el contenido de la página ni ningún enlace que lleve un token. En las páginas públicas (la página de inicio, el registro, el inicio de sesión, estas políticas y la pantalla que pregunta si quieres conectar un agente) también informa de qué botones y enlaces se pulsan; así distinguimos una página que se entiende bien de una que no.",
	"legal.privacy.where.analytics.sessions": "La página de inicio y la pantalla para conectar un agente graban además la propia sesión, para ver dónde se atasca alguien que entra por primera vez. Todo lo que se escribe en un campo se enmascara antes de salir del navegador, y nunca se graba ninguna página a la que llegues después de iniciar sesión.",
	"legal.privacy.where.hosting":            "<strong>Alojamiento, almacenamiento y correo</strong>: los servidores en los que funciona la aplicación, el almacenamiento de objetos para los archivos que subes y un proveedor de correo para los mensajes de la cuenta, como el restablecimiento de contraseña.",

	"legal.privacy.kept.h":  "Cuánto tiempo se conserva",
	"legal.privacy.kept.p1": "Hasta que lo eliminas. Las conversaciones largas se resumen en lugar de descartarse, porque la gracia del producto es que recuerda; por eso, una conversación de hace meses puede seguir existiendo como resumen después de que se compacten sus mensajes.",
	"legal.privacy.kept.p2": "Eliminar tu cuenta elimina tus datos. Las copias de seguridad y los registros que contengan datos de forma incidental pueden conservarse durante un breve periodo antes de rotar.",

	"legal.privacy.rights.h":      "Qué puedes hacer",
	"legal.privacy.rights.p":      "Según el RGPD del Reino Unido y de la UE, puedes acceder a tus datos, rectificarlos, suprimirlos, exportarlos, limitar su tratamiento u oponerte a él, y retirar tu consentimiento. Dos de estas cosas no necesitan ninguna solicitud:",
	"legal.privacy.rights.export": "<strong>Exportar</strong>: Ajustes → un archivo completo con tus datos, que se descarga al momento.",
	"legal.privacy.rights.delete": "<strong>Eliminar</strong>: Ajustes → eliminar cuenta.",
	"legal.privacy.rights.other":  "Para cualquier otra cosa, escribe a <a href=\"mailto:%[1]s\">%[1]s</a>. Si no estás conforme con la respuesta, puedes reclamar ante tu autoridad de protección de datos; en el Reino Unido, la Information Commissioner's Office.",

	"legal.privacy.security.h":  "Seguridad, sin exagerar",
	"legal.privacy.security.p1": "Las contraseñas se guardan como hash. Los tokens de las integraciones se cifran en reposo. Las sesiones están protegidas contra la falsificación de peticiones entre sitios (CSRF) y puedes ver el historial de inicios de sesión de tu cuenta.",
	"legal.privacy.security.p2": "Khepri lo construye y lo gestiona un equipo muy pequeño. No ha pasado una auditoría independiente ni tiene ninguna certificación de seguridad. Si eso no te parece un custodio aceptable para tus datos de salud, el consejo honesto es que no los guardes aquí.",

	"legal.privacy.changes.h": "Cambios",
	"legal.privacy.changes.p": "Los cambios importantes se anunciarán en la aplicación antes de que entren en vigor. La fecha al principio de esta página indica cuándo cambió por última vez.",

	"legal.terms.page":  "Términos",
	"legal.terms.title": "Términos del servicio",
	"legal.terms.lead":  "El acuerdo entre tú y %[1]s sobre tu uso de Khepri. Términos claros, y una sección que importa más que el resto: Khepri no es un médico.",

	"legal.terms.medical.h":      "No es consejo médico",
	"legal.terms.medical.p1":     "Khepri es una herramienta personal de coaching y de diario. No es un producto sanitario, y nada de lo que produce (planes de entrenamiento, objetivos de calorías, observaciones sobre tu sueño o cualquier cosa que diga el entrenador) es consejo médico, nutricional ni psicológico.",
	"legal.terms.medical.p2":     "Sus respuestas las generan modelos de lenguaje. Pueden equivocarse con total seguridad, y pueden equivocarse precisamente sobre ti. No uses Khepri para diagnosticar ni tratar nada, no dejes ni empieces un tratamiento por algo que haya dicho, y habla con un profesional cualificado antes de hacer un cambio importante en cómo comes, entrenas o te medicas.",
	"legal.terms.medical.crisis": "<strong>Si estás en crisis o puedes estar en peligro, contacta con los servicios de emergencia de tu zona o con una línea de ayuda en crisis. Ninguna persona supervisa Khepri y no puede ayudarte.</strong>",

	"legal.terms.account.h": "Tu cuenta",
	"legal.terms.account.p": "Debes tener al menos 16 años, dar datos correctos, no compartir tus credenciales y seguir siendo responsable de lo que ocurra en tu cuenta. Cada cuenta es para una sola persona.",

	"legal.terms.use.h":       "Uso aceptable",
	"legal.terms.use.intro":   "No debes:",
	"legal.terms.use.rights":  "subir nada que no tengas derecho a subir;",
	"legal.terms.use.others":  "subir datos de salud de otra persona sin su consentimiento;",
	"legal.terms.use.probe":   "intentar acceder a los datos de otra cuenta, ni romper, sobrecargar o sondear el servicio;",
	"legal.terms.use.resell":  "revender el servicio ni usarlo para construir un conjunto de datos de la competencia;",
	"legal.terms.use.illegal": "usarlo para nada ilegal.",
	"legal.terms.use.suspend": "Las cuentas que hagan estas cosas pueden suspenderse sin previo aviso.",

	"legal.terms.content.h":  "Tu contenido sigue siendo tuyo",
	"legal.terms.content.p1": "Conservas todos los derechos que tengas sobre lo que escribes y subes. Concedes a %[1]s solo el permiso necesario para prestarte el servicio: guardar tu contenido, procesarlo, enviar las partes relevantes al proveedor de IA que atiende tu cuenta y mostrártelo de nuevo.",
	"legal.terms.content.p2": "Tu contenido no se usa para entrenar modelos. Ten en cuenta que el proveedor de IA que lo recibe tiene sus propios términos, y que si usas tu propia clave de API, esos términos son entre tú y ese proveedor.",

	"legal.terms.avail.h":  "Disponibilidad, y qué debes dar por hecho",
	"legal.terms.avail.p1": "Khepri es software en una fase temprana que se ofrece tal cual, sin garantía de ningún tipo. No hay compromiso de disponibilidad. Las funciones pueden cambiar o desaparecer. Puede perder datos.",
	"legal.terms.avail.p2": "<strong>Guarda tu propia copia de todo lo que no te puedas permitir perder.</strong> Ajustes → Exportar genera un archivo completo en cualquier momento, y usarlo de vez en cuando es lo sensato.",

	"legal.terms.paid.h": "Planes de pago",
	"legal.terms.paid.p": "Khepri es gratuito por ahora. Si se introducen planes de pago, las condiciones, los precios y cualquier efecto sobre las cuentas existentes se publicarán antes de aplicarse, y no se te cobrará nada sin que lo aceptes primero.",

	"legal.terms.ending.h": "Darlo por terminado",
	"legal.terms.ending.p": "Puedes eliminar tu cuenta en cualquier momento desde Ajustes, lo que elimina tus datos tal como se describe en la <a href=\"/privacy\">política de privacidad</a>. %[1]s puede suspender o cerrar una cuenta que incumpla estos términos, o dejar de prestar el servicio con un preaviso razonable y la oportunidad de exportar.",

	"legal.terms.liability.h":  "Responsabilidad",
	"legal.terms.liability.p1": "En la máxima medida que permita la ley, %[1]s no responde de pérdidas indirectas o consecuentes, de la pérdida de datos ni del lucro cesante, y la responsabilidad total por cualquier reclamación se limita a la cantidad mayor entre lo que hayas pagado en los doce meses anteriores y 50 £.",
	"legal.terms.liability.p2": "Nada de lo anterior limita la responsabilidad por muerte o lesiones personales causadas por negligencia, por fraude ni por cualquier otra cosa que no pueda limitarse legalmente. Si eres consumidor, tus derechos legales no se ven afectados.",

	"legal.terms.law.h": "Ley aplicable",
	"legal.terms.law.p": "Estos términos se rigen por las leyes de %[1]s, cuyos tribunales tienen jurisdicción exclusiva, sin privar a un consumidor de la protección de las normas imperativas de su país de residencia.",

	"legal.terms.contact.h": "Contacto",

	"legal.fb.page":              "Datos de Facebook",
	"legal.fb.title":             "Tus datos de Facebook en Khepri",
	"legal.fb.lead":              "Khepri usa Facebook solo para encontrar a amigos tuyos que ya están aquí. Eres tú quien decide conectarlo desde Amigos, y puedes desconectarlo en cualquier momento.",
	"legal.fb.keeps.h":           "Qué guarda Khepri",
	"legal.fb.keeps.p1":          "El identificador de tu cuenta de Facebook propio de la app, vinculado a tu cuenta de Khepri, para que los amigos que se conecten más tarde puedan encontrarte. Nada más: ni tu nombre ni tu foto de Facebook, ni tu lista de amigos, ni el token de acceso que Facebook entrega mientras conectas. Ese token se usa una sola vez para ver cuáles de tus amigos usan Khepri y después se descarta.",
	"legal.fb.keeps.p2":          "Las cuentas de Khepri que encontró una conexión se guardan durante una hora para que puedas seguirlas, y después se eliminan.",
	"legal.fb.delete.h":          "Cómo eliminarlos",
	"legal.fb.delete.disconnect": "Inicia sesión en Khepri, abre <a href=\"/app/friends\">Amigos</a> y elige <strong>Desconectar Facebook</strong>. El vínculo y los resultados se eliminan al instante.",
	"legal.fb.delete.account":    "O elimina tu cuenta de Khepri en <a href=\"/app/settings\">Ajustes</a>, lo que lo elimina todo, incluido el vínculo con Facebook.",
	"legal.fb.delete.facebook":   "También puedes quitar Khepri desde el propio Facebook, en Configuración y privacidad → Configuración → Apps y sitios web. Así Khepri ya no puede leer nada nuevo de Facebook; desconéctalo también en Khepri para eliminar el vínculo que guarda.",
	"legal.fb.delete.email":      "Si no puedes iniciar sesión, escribe a %[1]s desde la dirección de tu cuenta y eliminaremos esos datos por ti.",
}
