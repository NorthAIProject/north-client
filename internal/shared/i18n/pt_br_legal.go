package i18n

var portugueseBrazilianLegal = map[string]string{
	"legal.updated":         "Última atualização: %[1]s",
	"legal.doc.translation": "Esta é uma tradução. A versão que vale é o texto em inglês; se os dois forem diferentes, prevalece o inglês.",
	"legal.foot.privacy":    "Privacidade",
	"legal.foot.terms":      "Termos",
	"legal.foot.home":       "Início",

	"legal.privacy.page":       "Privacidade",
	"legal.privacy.title":      "Política de privacidade",
	"legal.privacy.lead":       "O Khepri guarda uma quantidade incomum de informações sobre quem o usa — quanto pesa, como dormiu, por onde correu e o que escreveu num diário às onze da noite. Esta página diz exatamente o que é armazenado, para onde vai e como recuperar ou excluir.",
	"legal.privacy.controller": "%[1]s (%[2]s) é o controlador desses dados. Perguntas, solicitações e reclamações: <a href=\"mailto:%[3]s\">%[3]s</a>.",

	"legal.privacy.stored.h":            "O que é armazenado",
	"legal.privacy.stored.p":            "Somente o que você, ou uma integração que você conectou, colocou ali. Nada é comprado de corretores de dados e nada é deduzido a partir de rastreamento de terceiros.",
	"legal.privacy.stored.account":      "<strong>Conta</strong> — endereço de e-mail, nome de exibição, um hash da senha (nunca a senha), fuso horário e preferência de tom do treinador. Se você entrar com o Google ou com uma chave de acesso, o identificador que esse provedor nos fornece.",
	"legal.privacy.stored.health":       "<strong>Dados de saúde e do corpo</strong> — peso, medidas corporais, sono, hidratação e qualquer leitura biométrica que você registrar ou que um dispositivo conectado enviar.",
	"legal.privacy.stored.activity":     "<strong>Atividade e localização</strong> — treinos, planos de treino, sessões de atividade e, se você conectar o Strava, suas atividades, incluindo os trajetos que elas contêm. Um trajeto é um dado de localização sobre onde você realmente esteve.",
	"legal.privacy.stored.nutrition":    "<strong>Nutrição</strong> — alimentos, planos de refeições e o que você registrou.",
	"legal.privacy.stored.writing":      "<strong>O que você escreve</strong> — registros diários, entradas de diário, decisões, objetivos e todas as mensagens que você troca com o treinador.",
	"legal.privacy.stored.memory":       "<strong>Memória</strong> — fatos duradouros que o sistema extrai das suas conversas para não precisar perguntar duas vezes. Você pode ver, editar, fixar e excluir esses fatos.",
	"legal.privacy.stored.documents":    "<strong>Documentos</strong> — os arquivos que você envia, o texto extraído deles e representações numéricas (embeddings) desse texto usadas na busca.",
	"legal.privacy.stored.integrations": "<strong>Integrações</strong> — tokens de acesso dos serviços que você conecta, criptografados em repouso; seu identificador de chat do Telegram, se você vinculá-lo; tokens emitidos para os agentes que você autoriza.",
	"legal.privacy.stored.operational":  "<strong>Registros operacionais</strong> — eventos de login, atividade da conta, uso e custo dos modelos e logs de erro.",

	"legal.privacy.consent.h":  "Dados de saúde precisam do seu consentimento explícito",
	"legal.privacy.consent.p1": "A maior parte da lista acima são dados pessoais comuns. Peso, sono, dados biométricos e qualquer coisa relacionada à saúde no seu diário são dados de <em>categoria especial</em> nos termos do artigo 9 do GDPR do Reino Unido e da UE, e são tratados <strong>somente com o seu consentimento explícito</strong>, dado quando você cria uma conta e quando os registra.",
	"legal.privacy.consent.p2": "Você pode retirar esse consentimento a qualquer momento excluindo os dados ou a sua conta. A retirada não desfaz o tratamento que já aconteceu, e sem ele o Khepri deixa de funcionar de forma útil — um treinador sem dados de saúde é uma janela de chat.",

	"legal.privacy.where.h":                  "Para onde vão seus dados",
	"legal.privacy.where.p":                  "O Khepri não é financiado por publicidade e não vende nada a ninguém. Os dados só chegam a outras empresas quando o produto não consegue funcionar de outra forma:",
	"legal.privacy.where.ai":                 "<strong>Provedores de IA.</strong> Esta é a informação mais importante. Para responder a você, o treinador envia ao modelo sua mensagem junto com o contexto que reuniu — que pode incluir seus objetivos, conversas recentes, registros diários, resumos de treino e de nutrição e trechos dos seus próprios documentos. A empresa que recebe esses dados depende do provedor configurado para a sua conta, e você pode, em vez disso, usar a sua própria chave de API; nesse caso, seus dados vão para o seu provedor, sob o seu próprio acordo com ele.",
	"legal.privacy.where.strava":             "<strong>Strava</strong> — somente se você conectar, e somente para ler suas atividades.",
	"legal.privacy.where.telegram":           "<strong>Telegram</strong> — somente se você vincular um chat. Suas mensagens e as respostas do treinador passam pelos servidores do Telegram, sob a política do próprio Telegram.",
	"legal.privacy.where.analytics.events":   "<strong>Análise do produto.</strong> Um pequeno número de eventos — conta criada, configuração inicial concluída, fonte conectada, resposta do treinador — registrados com o identificador da sua conta para saber se o produto funciona. Nenhum dado de saúde, nenhum conteúdo de mensagem, nenhum texto de documento.",
	"legal.privacy.where.analytics.pages":    "Seu navegador também informa as páginas que você abre. Dentro do aplicativo, é só o endereço, sem nada do que vem depois do <code>?</code>, então o conteúdo da página e qualquer link que carregue um token não são incluídos. Nas páginas públicas — a página inicial, o cadastro, o login, estas políticas e a tela que pergunta se você quer conectar um agente — ele também informa quais botões e links são clicados; é assim que distinguimos uma página que se lê bem de uma que não.",
	"legal.privacy.where.analytics.sessions": "A página inicial e a tela de conectar um agente também gravam a própria sessão, para vermos onde quem chega pela primeira vez fica travado. Tudo o que é digitado num campo é mascarado antes de sair do navegador, e nenhuma página que você acessa depois de entrar é gravada.",
	"legal.privacy.where.hosting":            "<strong>Hospedagem, armazenamento e e-mail</strong> — os servidores onde o aplicativo roda, o armazenamento de objetos para os arquivos que você envia e um provedor de e-mail para as mensagens da conta, como a redefinição de senha.",

	"legal.privacy.kept.h":  "Por quanto tempo é guardado",
	"legal.privacy.kept.p1": "Até você excluir. Conversas longas são resumidas em vez de descartadas, porque a proposta do produto é lembrar — então uma conversa de meses atrás pode continuar existindo como resumo depois que as mensagens individuais forem compactadas.",
	"legal.privacy.kept.p2": "Excluir sua conta remove seus dados. Backups e logs que contenham registros incidentais podem permanecer por um curto período antes de serem descartados no rodízio.",

	"legal.privacy.rights.h":      "O que você pode fazer",
	"legal.privacy.rights.p":      "Nos termos do GDPR do Reino Unido e da UE, você pode acessar, corrigir, excluir, exportar, restringir ou se opor ao tratamento dos seus dados, e retirar o consentimento. Dois desses direitos não precisam de nenhuma solicitação:",
	"legal.privacy.rights.export": "<strong>Exportar</strong> — Configurações → um arquivo completo dos seus dados, baixado na hora.",
	"legal.privacy.rights.delete": "<strong>Excluir</strong> — Configurações → excluir conta.",
	"legal.privacy.rights.other":  "Para qualquer outra coisa, escreva para <a href=\"mailto:%[1]s\">%[1]s</a>. Se não ficar satisfeito com a resposta, você pode reclamar à sua autoridade de proteção de dados — no Reino Unido, o Information Commissioner's Office.",

	"legal.privacy.security.h":  "Segurança, com honestidade",
	"legal.privacy.security.p1": "As senhas são armazenadas como hash. Os tokens das integrações são criptografados em repouso. As sessões são protegidas contra falsificação de solicitações entre sites (CSRF), e você pode ver o histórico de logins da sua conta.",
	"legal.privacy.security.p2": "O Khepri é construído e operado por uma equipe muito pequena. Ele não passou por auditoria independente nem tem certificação de segurança. Se isso não for um guardião aceitável para os seus dados de saúde, o conselho honesto é não armazená-los aqui.",

	"legal.privacy.changes.h": "Mudanças",
	"legal.privacy.changes.p": "Mudanças relevantes serão anunciadas no aplicativo antes de entrarem em vigor. A data no topo desta página indica quando ela mudou pela última vez.",

	"legal.terms.page":  "Termos",
	"legal.terms.title": "Termos de serviço",
	"legal.terms.lead":  "O acordo entre você e %[1]s sobre o seu uso do Khepri. Termos simples, e uma seção que importa mais do que as outras: o Khepri não é um médico.",

	"legal.terms.medical.h":      "Não é orientação médica",
	"legal.terms.medical.p1":     "O Khepri é uma ferramenta pessoal de coaching e de diário. Ele não é um dispositivo médico, e nada do que ele produz — planos de treino, metas de calorias, observações sobre o seu sono ou qualquer coisa que o treinador diga — é orientação médica, nutricional ou psicológica.",
	"legal.terms.medical.p2":     "As respostas dele são geradas por modelos de linguagem. Elas podem estar erradas com toda a confiança, e podem estar erradas especificamente sobre você. Não use o Khepri para diagnosticar ou tratar nada, não interrompa nem comece um tratamento por causa de algo que ele disse, e converse com um profissional qualificado antes de fazer uma mudança significativa na forma como você come, treina ou se medica.",
	"legal.terms.medical.crisis": "<strong>Se você estiver em crise ou puder estar em perigo, entre em contato com o serviço de emergência local ou com uma linha de apoio emocional. O Khepri não é acompanhado por uma pessoa e não pode ajudar você.</strong>",

	"legal.terms.account.h": "Sua conta",
	"legal.terms.account.p": "Você precisa ter pelo menos 16 anos, fornecer dados corretos, não compartilhar suas credenciais e continua responsável pelo que acontece na sua conta. Cada conta é de uma só pessoa.",

	"legal.terms.use.h":       "Uso aceitável",
	"legal.terms.use.intro":   "Você não deve:",
	"legal.terms.use.rights":  "enviar nada que você não tenha o direito de enviar;",
	"legal.terms.use.others":  "enviar dados de saúde de outra pessoa sem o consentimento dela;",
	"legal.terms.use.probe":   "tentar acessar os dados de outra conta, nem quebrar, sobrecarregar ou sondar o serviço;",
	"legal.terms.use.resell":  "revender o serviço, nem usá-lo para construir um conjunto de dados concorrente;",
	"legal.terms.use.illegal": "usar o serviço para qualquer fim ilegal.",
	"legal.terms.use.suspend": "Contas que fizerem essas coisas podem ser suspensas sem aviso.",

	"legal.terms.content.h":  "Seu conteúdo continua sendo seu",
	"legal.terms.content.p1": "Você mantém todos os direitos que tem sobre o que escreve e envia. Você concede a %[1]s apenas a permissão necessária para operar o serviço para você: armazenar seu conteúdo, processá-lo, enviar as partes relevantes ao provedor de IA que atende sua conta e mostrá-lo de volta a você.",
	"legal.terms.content.p2": "Seu conteúdo não é usado para treinar modelos. Observe que o provedor de IA que o recebe tem seus próprios termos, e que, se você usar sua própria chave de API, esses termos são entre você e esse provedor.",

	"legal.terms.avail.h":  "Disponibilidade, e o que você deve presumir",
	"legal.terms.avail.p1": "O Khepri é um software em fase inicial, oferecido no estado em que se encontra, sem garantia de nenhum tipo. Não há compromisso de disponibilidade. Recursos podem mudar ou ser removidos. Ele pode perder dados.",
	"legal.terms.avail.p2": "<strong>Guarde sua própria cópia de tudo o que você não pode se dar ao luxo de perder.</strong> Configurações → Exportar gera um arquivo completo a qualquer momento, e usá-lo periodicamente é a premissa sensata para trabalhar.",

	"legal.terms.paid.h": "Planos pagos",
	"legal.terms.paid.p": "O Khepri é gratuito por enquanto. Se planos pagos forem criados, os termos, os preços e qualquer efeito nas contas existentes serão publicados antes de valerem, e você não será cobrado sem concordar antes.",

	"legal.terms.ending.h": "Encerramento",
	"legal.terms.ending.p": "Você pode excluir sua conta a qualquer momento em Configurações, o que remove seus dados conforme descrito na <a href=\"/privacy\">política de privacidade</a>. %[1]s pode suspender ou encerrar uma conta que viole estes termos, ou descontinuar o serviço com aviso prévio razoável e a oportunidade de exportar.",

	"legal.terms.liability.h":  "Responsabilidade",
	"legal.terms.liability.p1": "Na máxima extensão permitida por lei, %[1]s não se responsabiliza por perdas indiretas ou consequenciais, perda de dados ou lucros cessantes, e a responsabilidade total por qualquer reclamação fica limitada ao maior entre o valor que você pagou nos doze meses anteriores e £50.",
	"legal.terms.liability.p2": "Nada aqui limita a responsabilidade por morte ou lesão corporal causada por negligência, por fraude ou por qualquer outra coisa que não possa ser limitada legalmente. Se você for consumidor, seus direitos legais não são afetados.",

	"legal.terms.law.h": "Lei aplicável",
	"legal.terms.law.p": "Estes termos são regidos pelas leis de %[1]s, cujos tribunais têm jurisdição exclusiva — sem privar o consumidor da proteção das normas obrigatórias do seu país de residência.",

	"legal.terms.contact.h": "Contato",

	"legal.fb.page":              "Dados do Facebook",
	"legal.fb.title":             "Seus dados do Facebook no Khepri",
	"legal.fb.lead":              "O Khepri usa o Facebook somente para encontrar amigos seus que já estão aqui. É você quem escolhe conectá-lo em Amigos, e pode desconectar a qualquer momento.",
	"legal.fb.keeps.h":           "O que o Khepri guarda",
	"legal.fb.keeps.p1":          "O ID da sua conta do Facebook específico do app, vinculado à sua conta do Khepri, para que amigos que se conectarem depois possam encontrar você. Mais nada: nem seu nome ou foto do Facebook, nem sua lista de amigos, nem o token de acesso que o Facebook entrega enquanto você se conecta. Esse token é usado uma única vez para ver quais dos seus amigos usam o Khepri e depois é descartado.",
	"legal.fb.keeps.p2":          "As contas do Khepri encontradas por uma conexão ficam guardadas por uma hora para que você possa segui-las, e depois são excluídas.",
	"legal.fb.delete.h":          "Como excluir",
	"legal.fb.delete.disconnect": "Entre no Khepri, abra <a href=\"/app/friends\">Amigos</a> e escolha <strong>Desconectar o Facebook</strong>. O vínculo e quaisquer resultados são excluídos na hora.",
	"legal.fb.delete.account":    "Ou exclua sua conta do Khepri em <a href=\"/app/settings\">Configurações</a>, o que exclui tudo, inclusive o vínculo com o Facebook.",
	"legal.fb.delete.facebook":   "Você também pode remover o Khepri pelo próprio Facebook, em Configurações e privacidade → Configurações → Apps e sites. Assim o Khepri não consegue mais ler nada novo do Facebook; desconecte também no Khepri para excluir o vínculo que ele guarda.",
	"legal.fb.delete.email":      "Se você não conseguir entrar, escreva para %[1]s a partir do endereço da sua conta e excluiremos esses dados para você.",
}
