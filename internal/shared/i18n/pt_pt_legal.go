package i18n

var portugueseEuropeanLegal = map[string]string{
	"legal.updated":         "Última atualização: %[1]s",
	"legal.doc.translation": "Esta é uma tradução. A versão que se aplica é o texto em inglês; se os dois divergirem, prevalece o inglês.",
	"legal.foot.privacy":    "Privacidade",
	"legal.foot.terms":      "Termos",
	"legal.foot.home":       "Início",

	"legal.privacy.page":       "Privacidade",
	"legal.privacy.title":      "Política de privacidade",
	"legal.privacy.lead":       "O Khepri guarda uma quantidade invulgar de informação sobre quem o usa — quanto pesa, como dormiu, por onde correu e o que escreveu num diário às onze da noite. Esta página diz exatamente o que é guardado, para onde vai e como recuperá-lo ou apagá-lo.",
	"legal.privacy.controller": "%[1]s (%[2]s) é o responsável pelo tratamento desses dados. Perguntas, pedidos e reclamações: <a href=\"mailto:%[3]s\">%[3]s</a>.",

	"legal.privacy.stored.h":            "O que é guardado",
	"legal.privacy.stored.p":            "Apenas o que tu, ou uma integração que ligaste, lá colocaram. Nada é comprado a corretores de dados e nada é inferido a partir de rastreio de terceiros.",
	"legal.privacy.stored.account":      "<strong>Conta</strong> — endereço de email, nome de apresentação, um hash da palavra-passe (nunca a palavra-passe), fuso horário e preferência de tom do treinador. Se iniciares sessão com o Google ou com uma chave de acesso, o identificador que esse fornecedor nos dá.",
	"legal.privacy.stored.health":       "<strong>Dados de saúde e do corpo</strong> — peso, medidas corporais, sono, hidratação e quaisquer leituras biométricas que registes ou que um dispositivo ligado envie.",
	"legal.privacy.stored.activity":     "<strong>Atividade e localização</strong> — treinos, planos de treino, sessões de atividade e, se ligares o Strava, as tuas atividades, incluindo os percursos que contêm. Um percurso é um dado de localização sobre onde realmente estiveste.",
	"legal.privacy.stored.nutrition":    "<strong>Nutrição</strong> — alimentos, planos de refeições e o que registaste.",
	"legal.privacy.stored.writing":      "<strong>O que escreves</strong> — registos diários, entradas do diário, decisões, objetivos e todas as mensagens que trocas com o treinador.",
	"legal.privacy.stored.memory":       "<strong>Memória</strong> — factos duradouros que o sistema retira das tuas conversas para não ter de perguntar duas vezes. Podes vê-los, editá-los, fixá-los e apagá-los.",
	"legal.privacy.stored.documents":    "<strong>Documentos</strong> — os ficheiros que carregas, o texto extraído deles e representações numéricas (embeddings) desse texto usadas na pesquisa.",
	"legal.privacy.stored.integrations": "<strong>Integrações</strong> — tokens de acesso dos serviços que ligas, cifrados em repouso; o teu identificador de chat do Telegram, se o associares; tokens emitidos para os agentes que autorizas.",
	"legal.privacy.stored.operational":  "<strong>Registos operacionais</strong> — inícios de sessão, atividade da conta, utilização e custo dos modelos, e registos de erros.",

	"legal.privacy.consent.h":  "Os dados de saúde precisam do teu consentimento explícito",
	"legal.privacy.consent.p1": "A maior parte da lista acima são dados pessoais comuns. O peso, o sono, os dados biométricos e tudo o que no teu diário diga respeito à saúde são dados de <em>categoria especial</em> ao abrigo do artigo 9.º do RGPD do Reino Unido e da UE, e são tratados <strong>apenas com o teu consentimento explícito</strong>, dado quando crias uma conta e quando os registas.",
	"legal.privacy.consent.p2": "Podes retirar esse consentimento a qualquer momento, apagando os dados ou a tua conta. A retirada não desfaz o tratamento que já aconteceu, e sem ele o Khepri deixa de funcionar de forma útil — um treinador sem dados de saúde é uma janela de chat.",

	"legal.privacy.where.h":                  "Para onde vão os teus dados",
	"legal.privacy.where.p":                  "O Khepri não é financiado por publicidade e não vende nada a ninguém. Os dados só chegam a outras empresas quando o produto não consegue funcionar de outra forma:",
	"legal.privacy.where.ai":                 "<strong>Fornecedores de IA.</strong> Esta é a informação mais importante. Para te responder, o treinador envia ao modelo a tua mensagem juntamente com o contexto que reuniu — que pode incluir os teus objetivos, conversas recentes, registos diários, resumos de treino e de nutrição, e excertos dos teus próprios documentos. A empresa que os recebe depende do fornecedor configurado para a tua conta, e podes, em alternativa, usar a tua própria chave de API; nesse caso, os teus dados vão para o teu fornecedor, ao abrigo do teu próprio acordo com ele.",
	"legal.privacy.where.strava":             "<strong>Strava</strong> — apenas se o ligares, e apenas para ler as tuas atividades.",
	"legal.privacy.where.telegram":           "<strong>Telegram</strong> — apenas se associares um chat. As tuas mensagens e as respostas do treinador passam pelos servidores do Telegram, ao abrigo da política do próprio Telegram.",
	"legal.privacy.where.analytics.events":   "<strong>Análise do produto.</strong> Um pequeno número de eventos — conta criada, configuração inicial concluída, fonte ligada, resposta do treinador — registados com o identificador da tua conta para perceber se o produto funciona. Nada de dados de saúde, conteúdo de mensagens ou texto de documentos.",
	"legal.privacy.where.analytics.pages":    "O teu navegador também comunica as páginas que abres. Dentro da aplicação, é apenas o endereço, sem nada do que vem depois do <code>?</code>, por isso o conteúdo da página e qualquer link que transporte um token não são incluídos. Nas páginas públicas — a página inicial, o registo, o início de sessão, estas políticas e o ecrã que pergunta se queres ligar um agente — também comunica que botões e links são clicados; é assim que distinguimos uma página que se lê bem de uma que não.",
	"legal.privacy.where.analytics.sessions": "A página inicial e o ecrã de ligação de um agente gravam ainda a própria sessão, para vermos onde alguém que chega pela primeira vez fica preso. Tudo o que é escrito num campo é ocultado antes de sair do navegador, e nenhuma página a que chegues depois de iniciar sessão é alguma vez gravada.",
	"legal.privacy.where.hosting":            "<strong>Alojamento, armazenamento e email</strong> — os servidores onde a aplicação corre, o armazenamento de objetos para os ficheiros que carregas e um fornecedor de email para as mensagens da conta, como a reposição da palavra-passe.",

	"legal.privacy.kept.h":  "Durante quanto tempo é guardado",
	"legal.privacy.kept.p1": "Até o apagares. As conversas longas são resumidas em vez de descartadas, porque o propósito do produto é lembrar-se — por isso, uma conversa de há meses pode continuar a existir como resumo depois de as suas mensagens individuais serem compactadas.",
	"legal.privacy.kept.p2": "Apagar a tua conta remove os teus dados. Cópias de segurança e registos que contenham dados de forma incidental podem manter-se durante um curto período antes de serem rodados.",

	"legal.privacy.rights.h":      "O que podes fazer",
	"legal.privacy.rights.p":      "Ao abrigo do RGPD do Reino Unido e da UE, podes aceder aos teus dados, retificá-los, apagá-los, exportá-los, limitar o seu tratamento ou opor-te a ele, e retirar o teu consentimento. Dois destes direitos não precisam de qualquer pedido:",
	"legal.privacy.rights.export": "<strong>Exportar</strong> — Definições → um arquivo completo dos teus dados, descarregado de imediato.",
	"legal.privacy.rights.delete": "<strong>Apagar</strong> — Definições → apagar conta.",
	"legal.privacy.rights.other":  "Para tudo o resto, escreve para <a href=\"mailto:%[1]s\">%[1]s</a>. Se não estiveres satisfeito com a resposta, podes apresentar reclamação à tua autoridade de proteção de dados — no Reino Unido, o Information Commissioner's Office.",

	"legal.privacy.security.h":  "Segurança, dita com honestidade",
	"legal.privacy.security.p1": "As palavras-passe são guardadas como hash. Os tokens das integrações são cifrados em repouso. As sessões estão protegidas contra falsificação de pedidos entre sites (CSRF), e podes ver o histórico de inícios de sessão da tua conta.",
	"legal.privacy.security.p2": "O Khepri é construído e gerido por uma equipa muito pequena. Não foi auditado de forma independente nem tem qualquer certificação de segurança. Se isso não for um guardião aceitável para os teus dados de saúde, o conselho honesto é não os guardares aqui.",

	"legal.privacy.changes.h": "Alterações",
	"legal.privacy.changes.p": "As alterações relevantes serão anunciadas na aplicação antes de entrarem em vigor. A data no topo desta página indica quando foi alterada pela última vez.",

	"legal.terms.page":  "Termos",
	"legal.terms.title": "Termos de serviço",
	"legal.terms.lead":  "O acordo entre ti e %[1]s relativo à tua utilização do Khepri. Termos simples, e uma secção que importa mais do que as outras: o Khepri não é um médico.",

	"legal.terms.medical.h":      "Não é aconselhamento médico",
	"legal.terms.medical.p1":     "O Khepri é uma ferramenta pessoal de coaching e de diário. Não é um dispositivo médico, e nada do que produz — planos de treino, metas de calorias, observações sobre o teu sono ou qualquer coisa que o treinador diga — constitui aconselhamento médico, nutricional ou psicológico.",
	"legal.terms.medical.p2":     "As suas respostas são geradas por modelos de linguagem. Podem estar erradas com toda a confiança, e podem estar erradas especificamente sobre ti. Não uses o Khepri para diagnosticar ou tratar o que quer que seja, não interrompas nem comeces um tratamento por causa de algo que ele disse, e fala com um profissional qualificado antes de fazeres uma mudança significativa na forma como comes, treinas ou tomas medicação.",
	"legal.terms.medical.crisis": "<strong>Se estiveres em crise ou em perigo, contacta os serviços de emergência locais ou uma linha de apoio em crise. O Khepri não é acompanhado por uma pessoa e não te pode ajudar.</strong>",

	"legal.terms.account.h": "A tua conta",
	"legal.terms.account.p": "Tens de ter pelo menos 16 anos, fornecer dados corretos, não partilhar as tuas credenciais e continuas responsável pelo que acontece na tua conta. Cada conta é para uma só pessoa.",

	"legal.terms.use.h":       "Utilização aceitável",
	"legal.terms.use.intro":   "Não deves:",
	"legal.terms.use.rights":  "carregar nada que não tenhas o direito de carregar;",
	"legal.terms.use.others":  "carregar dados de saúde de outra pessoa sem o seu consentimento;",
	"legal.terms.use.probe":   "tentar aceder aos dados de outra conta, ou quebrar, sobrecarregar ou sondar o serviço;",
	"legal.terms.use.resell":  "revender o serviço, ou usá-lo para construir um conjunto de dados concorrente;",
	"legal.terms.use.illegal": "usá-lo para qualquer fim ilegal.",
	"legal.terms.use.suspend": "As contas que façam estas coisas podem ser suspensas sem aviso prévio.",

	"legal.terms.content.h":  "O teu conteúdo continua a ser teu",
	"legal.terms.content.p1": "Manténs todos os direitos que tens sobre o que escreves e carregas. Concedes a %[1]s apenas a permissão necessária para te prestar o serviço: guardar o teu conteúdo, processá-lo, enviar as partes relevantes ao fornecedor de IA que serve a tua conta e mostrá-lo de volta.",
	"legal.terms.content.p2": "O teu conteúdo não é usado para treinar modelos. Nota que o fornecedor de IA que o recebe tem os seus próprios termos, e que, se usares a tua própria chave de API, esses termos são entre ti e esse fornecedor.",

	"legal.terms.avail.h":  "Disponibilidade, e o que deves assumir",
	"legal.terms.avail.p1": "O Khepri é software numa fase inicial, disponibilizado tal como está, sem garantia de qualquer tipo. Não há compromisso de disponibilidade. As funcionalidades podem mudar ou ser removidas. Pode perder dados.",
	"legal.terms.avail.p2": "<strong>Guarda a tua própria cópia de tudo o que não te possas dar ao luxo de perder.</strong> Definições → Exportar cria um arquivo completo a qualquer momento, e usá-lo periodicamente é o pressuposto sensato com que trabalhar.",

	"legal.terms.paid.h": "Planos pagos",
	"legal.terms.paid.p": "O Khepri é, por agora, gratuito. Se forem introduzidos planos pagos, os termos, os preços e qualquer efeito nas contas existentes serão publicados antes de se aplicarem, e não te será cobrado nada sem que concordes primeiro.",

	"legal.terms.ending.h": "Terminar",
	"legal.terms.ending.p": "Podes apagar a tua conta a qualquer momento nas Definições, o que remove os teus dados conforme descrito na <a href=\"/privacy\">política de privacidade</a>. %[1]s pode suspender ou encerrar uma conta que viole estes termos, ou descontinuar o serviço com um aviso prévio razoável e a oportunidade de exportar.",

	"legal.terms.liability.h":  "Responsabilidade",
	"legal.terms.liability.p1": "Na máxima medida permitida por lei, %[1]s não é responsável por perdas indiretas ou consequenciais, perda de dados ou lucros cessantes, e a responsabilidade total por qualquer reclamação está limitada ao maior dos seguintes valores: o montante que pagaste nos doze meses anteriores ou 50 £.",
	"legal.terms.liability.p2": "Nada aqui limita a responsabilidade por morte ou danos pessoais causados por negligência, por fraude, ou por qualquer outra coisa que não possa ser legalmente limitada. Se fores consumidor, os teus direitos legais não são afetados.",

	"legal.terms.law.h": "Lei aplicável",
	"legal.terms.law.p": "Estes termos regem-se pelas leis de %[1]s, cujos tribunais têm jurisdição exclusiva — sem privar um consumidor da proteção das normas imperativas do seu país de residência.",

	"legal.terms.contact.h": "Contacto",

	"legal.fb.page":              "Dados do Facebook",
	"legal.fb.title":             "Os teus dados do Facebook no Khepri",
	"legal.fb.lead":              "O Khepri usa o Facebook apenas para encontrar amigos teus que já cá estão. És tu que escolhes ligá-lo a partir de Amigos, e podes desligá-lo a qualquer momento.",
	"legal.fb.keeps.h":           "O que o Khepri guarda",
	"legal.fb.keeps.p1":          "O ID da tua conta do Facebook específico da app, associado à tua conta do Khepri, para que os amigos que se liguem mais tarde te possam encontrar. Mais nada: nem o teu nome ou fotografia do Facebook, nem a tua lista de amigos, nem o token de acesso que o Facebook entrega enquanto te ligas. Esse token é usado uma única vez para ver quais dos teus amigos usam o Khepri e depois é descartado.",
	"legal.fb.keeps.p2":          "As contas do Khepri encontradas por uma ligação são guardadas durante uma hora para as poderes seguir, e depois apagadas.",
	"legal.fb.delete.h":          "Como apagá-los",
	"legal.fb.delete.disconnect": "Inicia sessão no Khepri, abre <a href=\"/app/friends\">Amigos</a> e escolhe <strong>Desligar o Facebook</strong>. A associação e quaisquer resultados são apagados de imediato.",
	"legal.fb.delete.account":    "Ou apaga a tua conta do Khepri em <a href=\"/app/settings\">Definições</a>, o que apaga tudo, incluindo a associação ao Facebook.",
	"legal.fb.delete.facebook":   "Também podes remover o Khepri a partir do próprio Facebook, em Definições e privacidade → Definições → Apps e sites. O Khepri deixa então de conseguir ler algo novo do Facebook; desliga-o também no Khepri para apagar a associação que guarda.",
	"legal.fb.delete.email":      "Se não conseguires iniciar sessão, escreve para %[1]s a partir do endereço da tua conta e apagamos esses dados por ti.",
}
