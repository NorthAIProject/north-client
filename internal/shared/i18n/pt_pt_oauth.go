package i18n

// portugueseEuropeanOAuth is the agent consent screen in European Portuguese.
var portugueseEuropeanOAuth = map[string]string{
	"oauth.title": "Ligar um agente",

	"oauth.asking":            "%[1]s quer ligar-se à tua conta Khepri.",
	"oauth.destination":       "Vai enviar o teu acesso para %[1]s.",
	"oauth.destination.local": "Vai enviar o teu acesso para este computador.",

	"oauth.account.signed-in": "Sessão iniciada como %[1]s",
	"oauth.account.notyou":    "Não és tu?",

	"oauth.can.title":         "O que vai poder fazer",
	"oauth.can.read":          "Ler os teus objetivos, registos diários, treino, factos memorizados e documentos.",
	"oauth.can.write":         "Registar check-ins, concluir hábitos, registar o teu peso, registar refeições e editar o teu plano de treino.",
	"oauth.can.readonly.note": "Não vai poder alterar nada.",

	"oauth.readonly.label": "Apenas leitura",
	"oauth.readonly.hint":  "Deixa-o ler os teus dados sem alterar nada.",

	"oauth.approve":     "Aprovar",
	"oauth.deny":        "Cancelar",
	"oauth.revoke.note": "Podes revogar isto quando quiseres em Definições → Ligações.",

	"oauth.new.title":    "Cria a tua conta Khepri",
	"oauth.new.desc":     "Estás a dar a um agente uma memória que dura. Precisa de um sítio para a guardar.",
	"oauth.new.name":     "O teu nome",
	"oauth.new.email":    "Email",
	"oauth.new.password": "Palavra-passe",
	"oauth.new.submit":   "Criar conta e continuar",
	"oauth.new.haveone":  "Já tens conta?",

	"oauth.signin.title":  "Inicia sessão para continuar",
	"oauth.signin.submit": "Iniciar sessão",
	"oauth.signin.new":    "Precisas de uma conta?",

	"oauth.or":      "ou",
	"oauth.google":  "Continuar com Google",
	"oauth.passkey": "Continuar com uma passkey",

	"oauth.error.title":          "Não é possível completar esse pedido",
	"oauth.error.unknown-client": "A aplicação que faz este pedido não está registada no Khepri.",
	"oauth.error.bad-redirect":   "Este pedido queria enviar o teu acesso para um sítio que a aplicação não registou.",
	"oauth.error.no-client":      "Este pedido não disse qual é a aplicação que está a pedir.",
	"oauth.error.expired":        "Este pedido expirou. Começa outra vez a partir do teu agente.",
	"oauth.error.home":           "Ir para o Khepri",
}
