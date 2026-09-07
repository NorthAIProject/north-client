package i18n

// portugueseBrazilianOAuth is the agent consent screen in Brazilian
// Portuguese.
//
// Kept separate from pt-PT rather than fallback-chained, for the vocabulary
// reasons the locale set already documents: "senha" and "palavra-passe" are
// not interchangeable, and neither are the second-person forms.
var portugueseBrazilianOAuth = map[string]string{
	"oauth.title": "Conectar um agente",

	"oauth.asking":            "%[1]s quer se conectar à sua conta Khepri.",
	"oauth.destination":       "Vai enviar seu acesso para %[1]s.",
	"oauth.destination.local": "Vai enviar seu acesso para este computador.",

	"oauth.account.signed-in": "Conectado como %[1]s",
	"oauth.account.notyou":    "Não é você?",

	"oauth.can.title":         "O que vai poder fazer",
	"oauth.can.read":          "Ler suas metas, check-ins, treinos, fatos memorizados e documentos.",
	"oauth.can.write":         "Registrar check-ins, concluir hábitos, registrar seu peso, registrar refeições e editar seu plano de treino.",
	"oauth.can.readonly.note": "Não vai poder alterar nada.",

	"oauth.readonly.label": "Somente leitura",
	"oauth.readonly.hint":  "Deixe ler seus dados sem alterar nada.",

	"oauth.approve":     "Aprovar",
	"oauth.deny":        "Cancelar",
	"oauth.revoke.note": "Você pode revogar isso quando quiser em Configurações → Conexões.",

	"oauth.new.title":    "Crie sua conta Khepri",
	"oauth.new.desc":     "Você está dando a um agente uma memória que dura. Ela precisa de um lugar para ficar.",
	"oauth.new.name":     "Seu nome",
	"oauth.new.email":    "E-mail",
	"oauth.new.password": "Senha",
	"oauth.new.submit":   "Criar conta e continuar",
	"oauth.new.haveone":  "Já tem uma conta?",

	"oauth.signin.title":  "Entre para continuar",
	"oauth.signin.submit": "Entrar",
	"oauth.signin.new":    "Precisa de uma conta?",

	"oauth.or":      "ou",
	"oauth.google":  "Continuar com o Google",
	"oauth.passkey": "Continuar com uma passkey",

	"oauth.error.title":          "Não é possível concluir esse pedido",
	"oauth.error.unknown-client": "O aplicativo que faz este pedido não está registrado no Khepri.",
	"oauth.error.bad-redirect":   "Este pedido quis enviar seu acesso para um lugar que o aplicativo não registrou.",
	"oauth.error.no-client":      "Este pedido não disse qual aplicativo está pedindo.",
	"oauth.error.expired":        "Este pedido expirou. Comece de novo pelo seu agente.",
	"oauth.error.home":           "Ir para o Khepri",
}
