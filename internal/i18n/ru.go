// Package i18n holds every user-facing string of the bot.
package i18n

// Strings is the set of UI texts for one language.
type Strings struct {
	Greeting       string // %s: user's first name
	GreetingText   string // on the next line, after the greeting and its emoji
	ChooseSubject  string
	NoSubjects     string
	ChooseGrade    string // %s: subject label
	TopicsHeader   string // %s: subject label, %s: grade title
	ChooseTopic    string
	NoTopics       string
	GradeTitle     string // %s: grade directory name
	Back           string
	Home           string
	SuggestEdit    string
	AddTopic       string
	DownloadMD     string
	NotFound       string
	TooLarge       string
	SendFailed     string
	UnknownCommand string

	// Suggestions. Users are schoolchildren and teachers: no file names,
	// Markdown or LaTeX here, and every step offers «Отмена».
	SuggestDisabled    string
	SuggestMenu        string
	SuggestMenuMain    string
	AddNote            string
	ChooseKind         string
	ChooseParent       string
	NewNoteTitlePrompt string // %s: kind label, %s: topic title
	NameInvalid        string
	NameTaken          string
	SubmittedNote      string // %s: title
	Continued          string // %s: note title
	ArchiveName        string // %s: date
	FixText            string
	ReportError        string
	Cancel             string
	Send               string
	SendAnyway         string
	Done               string
	OpenTopic          string
	OpenNamed          string // %s: topic title
	ChooseSection      string
	SectionStart       string
	SectionPart        string // %s: section title, %d: part number
	EditInstructions   string
	EditEmptySection   string
	EditReceived       string
	EditUnchanged      string
	EditShrunk         string
	EditImageFirst     string // %d: pictures so far
	ImageAdded         string // %d: pictures so far
	ReportPrompt       string
	ReportReceived     string
	NewTitlePrompt     string // %s: subject label, %s: grade title
	NewTitleTaken      string // %s: existing topic title
	NewTitleTooLong    string
	NewTitleFirst      string
	NewTextPrompt      string
	NewImageFirst      string // %d: pictures so far
	NewImagesPrompt    string
	NewMoreText        string
	NewImageAdded      string // %d: pictures so far
	UseButtons         string
	OnlyTextAndImages  string
	ImageTooLarge      string
	TooManyImages      string // %d: limit
	TextTooLong        string
	Expired            string
	Cancelled          string
	NothingToCancel    string
	TooOften           string
	TooManyOpen        string // %d: open suggestions
	Submitting         string
	SubmittedEdit      string
	SubmittedReport    string
	SubmittedNew       string // %s: topic title
	SubmitFailed       string
	AcceptedEdit       string // %s: topic title
	AcceptedNew        string // %s: topic title
	AcceptedReport     string // %s: topic title
	AcceptedCount      string // %d: accepted suggestions
	RankUp             string // %s: rank (after its emoji, if any)
	RankUpPrefix       string
	RejectedEdit       string // %s: topic title
	RejectedNew        string // %s: topic title
	RejectedReport     string // %s: topic title
	ReviewComment      string // %s: comment

	// The keyboard at the bottom of the chat.
	KeySubjects string
	KeyPinned   string
	KeyAccount  string
	// KeyOld are the button texts of keyboards sent by earlier versions,
	// which stay on users' screens until the next /start: Предметы,
	// Закреплённые, Аккаунт.
	KeyOld [3]string
	Close  string

	// Pins.
	Pin         string
	Unpin       string
	PinDone     string
	UnpinDone   string
	PinLimit    string // %d: limit
	PinnedTitle string
	PinnedEmpty string

	// Account and the list of suggestions, sent as rich messages
	// (Markdown).
	AccountRank     string // %s: rank
	AccountNext     string // %d: accepted suggestions left, %s: «правка» in the right form
	AccountTop      string
	AccountAccepted string // %d: accepted suggestions
	AccountOpen     string // %d: open suggestions
	AccountAdmin    string
	Edits           [3]string // «правка», «правки», «правок»
	History         string
	HistoryPending  string
	HistoryAccepted string
	HistoryRejected string
	HistoryEmpty    string
	HistoryItem     string // %s: title, %s: kind, %s: date
	KindEditLabel   string
	KindTopicLabel  string
	KindNoteLabel   string
	KindReportLabel string

	// Edit access.
	AccessNeeded      string
	SendRequest       string
	AccessRequested   string
	AccessPending     string
	AccessDeniedUntil string // %s: date
	Understood        string
	AccessNoAdmins    string
	AccessHave        string
	AdminRequest      string // %s: who, %d: accepted edits, %d: accepted reports
	Allow             string
	Deny              string
	AdminAllowed      string // %s: admin
	AdminDenied       string // %s: admin
	AdminDecided      string // %s: admin
	AccessGranted     string
	AccessDenied      string
	AccessRevoked     string
	AccessGone        string
	AccessList        string // %d: count, %s: «человека» or «человек»
	AccessListEmpty   string
	AccessListLine    string // %s: user, %s: date, %s: admin
	AccessAdmins      string // %s: admins
	RevokeUsage       string
	RevokeDone        string // %s: user
	RevokeUnknown     string
	RevokeNoAccess    string
	RevokeAdmin       string

	// /emoji_setup: one step per place a custom emoji can go.
	EmojiStep          string // %d: step, %d: steps, %s: where
	EmojiWhereSubjects string
	EmojiWherePinned   string
	EmojiWhereAccount  string
	EmojiWherePin      string
	EmojiWhereGreeting string
	EmojiHintGreeting  string
	EmojiWhereSubject  string // %s: subject
	EmojiWhereRank     string // %s: rank, %d: accepted suggestions it starts at, %s: AcceptedFrom form
	EmojiHintRank      string
	EmojiShortSubjects string
	EmojiShortPinned   string
	EmojiShortAccount  string
	EmojiShortPin      string
	EmojiShortGreeting string
	EmojiShortSubject  string // %s: subject
	EmojiShortRank     string // %s: rank
	EmojiNow           string // followed by the emoji
	EmojiNowNone       string
	EmojiAsk           string
	EmojiSkip          string
	EmojiClear         string
	EmojiNeedCustom    string
	EmojiSaveQuestion  string
	Yes                string
	EmojiOther         string
	EmojiCancelled     string
	EmojiSaved         string
	EmojiNone          string
	EmojiUnknownSlot   string // %s: the slots
	EmojiSaveFailed    string // %s: error
	RanksTitle         string
	RanksLine          string    // %s: rank, %d: accepted suggestions, %s: AcceptedFrom form
	AcceptedFrom       [3]string // «от 1 принятой правки», «от 2 принятых правок»…
	RanksEdit          string

	// The command menu.
	CmdStart      string
	CmdDownload   string
	CmdCancel     string
	CmdAccess     string
	CmdRevoke     string
	CmdEmojiSetup string
	CmdRanks      string

	// Months in the genitive case, for dates such as «25 октября».
	Months [12]string
}

// RU is the Russian UI.
var RU = Strings{
	Greeting:       "Привет, %s!",
	GreetingText:   "Это terakoya — школьный справочник, который пишут сами пользователи.",
	ChooseSubject:  "Выберите предмет:",
	NoSubjects:     "Каталог пока пуст.",
	ChooseGrade:    "%s\n\nВыберите класс:",
	TopicsHeader:   "%s · %s",
	ChooseTopic:    "Выберите тему:",
	NoTopics:       "В этом классе пока нет тем.",
	GradeTitle:     "%s класс",
	Back:           "« Назад",
	Home:           "В начало",
	SuggestEdit:    "Предложить правку",
	AddTopic:       "Добавить тему",
	DownloadMD:     "Скачать",
	NotFound:       "Тема не найдена. Возможно, каталог обновился.",
	TooLarge:       "Эта тема слишком большая для одного сообщения. Нажмите «Скачать», чтобы получить её файлом.",
	SendFailed:     "Не удалось отправить тему. Попробуйте позже.",
	UnknownCommand: "Не понимаю. Нажмите /start, чтобы открыть каталог.",

	SuggestDisabled: "Предлагать правки пока нельзя.",
	SuggestMenu: "Что хотите сделать?\n\n" +
		"«Исправить текст» — поправите сами.\n" +
		"«Сообщить об ошибке» — просто опишите, что не так, а мы исправим.",
	SuggestMenuMain: "Что хотите сделать?\n\n" +
		"«Исправить текст» — поправите сами.\n" +
		"«Сообщить об ошибке» — просто опишите, что не так, а мы исправим.\n" +
		"«Добавить теорему, задачу…» — новая запись в этой теме.",
	AddNote:            "Добавить теорему, задачу…",
	ChooseKind:         "Что добавить?",
	ChooseParent:       "К чему это следствие?",
	NewNoteTitlePrompt: "Новая запись: %s, тема «%s».\n\nНапишите её название.",
	NameInvalid:        "Такое название не подойдёт — напишите его словами.",
	NameTaken:          "Такое название уже есть. Придумайте другое.",
	SubmittedNote:      "Спасибо! Мы проверим «%s» и добавим в тему. Я напишу, когда это случится.",
	Continued:          "%s (продолжение)",
	ArchiveName:        "Теракоя %s.zip",
	FixText:            "Исправить текст",
	ReportError:        "Сообщить об ошибке",
	Cancel:             "Отмена",
	Send:               "Отправить",
	SendAnyway:         "Отправить как есть",
	Done:               "Готово",
	OpenTopic:          "Открыть тему",
	OpenNamed:          "Открыть «%s»",
	ChooseSection:      "Статья длинная. Какую часть исправить?",
	SectionStart:       "Начало",
	SectionPart:        "%s (%d)",
	EditInstructions:   "Скопируйте текст выше (нажмите на него → «Копировать»), исправьте и пришлите мне.",
	EditEmptySection:   "Здесь пока нет текста. Напишите его и пришлите мне.",
	EditReceived:       "Получил! Если нужно, пришлите картинку — можно с подписью, куда её поставить.",
	EditUnchanged:      "Текст не изменился. Исправьте что-нибудь и пришлите ещё раз.",
	EditShrunk: "Похоже, вы прислали только часть текста — остальное будет удалено.\n\n" +
		"Если хотели исправить одно место, пришлите весь текст целиком, вместе с исправлением.",
	EditImageFirst:  "Картинка добавлена (всего: %d). Теперь пришлите исправленный текст.",
	ImageAdded:      "Картинка добавлена (всего: %d). Можно прислать ещё или отправить.",
	ReportPrompt:    "Опишите своими словами, что не так. Например: «в теореме опечатка, должно быть 180°». Можно приложить фото или скриншот.",
	ReportReceived:  "Понял! Можно добавить ещё фото или сразу отправить.",
	NewTitlePrompt:  "Новая тема: %s · %s. Как она называется?",
	NewTitleTaken:   "«%s» уже есть. Пришлите другое название или откройте то, что есть.",
	NewTitleTooLong: "Слишком длинное название — сократите, пожалуйста.",
	NewTitleFirst:   "Сначала напишите, как называется тема.",
	NewTextPrompt:   "Отлично! Теперь пришлите текст темы.",
	NewImageFirst:   "Картинка добавлена (всего: %d). Теперь пришлите текст темы.",
	NewImagesPrompt: "Есть картинки? Пришлите их — можно с подписью, например «после первого абзаца». Или нажмите «Готово».\n\n" +
		"Если текст не поместился в одно сообщение, пришлите продолжение.",
	NewMoreText:       "Добавил продолжение. Пришлите ещё или нажмите «Готово».",
	NewImageAdded:     "Картинок: %d. Пришлите ещё или нажмите «Готово».",
	UseButtons:        "Выберите вариант кнопкой выше.",
	OnlyTextAndImages: "Я понимаю только текст и картинки.",
	ImageTooLarge:     "Эта картинка слишком большая — пришлите поменьше.",
	TooManyImages:     "Больше %d картинок прислать нельзя.",
	TextTooLong:       "Слишком длинный текст — пришлите его частями поменьше.",
	Expired:           "Время вышло — начните заново кнопкой «Предложить правку».",
	Cancelled:         "Хорошо, отменил.",
	NothingToCancel:   "Нечего отменять.",
	TooOften:          "Подождите минутку перед следующим предложением.",
	TooManyOpen:       "Ваших предложений на проверке: %d. Подождите, пока мы их проверим, и возвращайтесь.",
	Submitting:        "Отправляю…",
	SubmittedEdit:     "Спасибо! Мы проверим правку и напишем вам.",
	SubmittedReport:   "Спасибо! Мы посмотрим и исправим. Я напишу, когда это случится.",
	SubmittedNew:      "Спасибо! Мы проверим тему «%s» и добавим её. Я напишу, когда это случится.",
	SubmitFailed:      "Не получилось отправить. Попробуйте ещё раз чуть позже.",
	AcceptedEdit:      "Ваша правка в «%s» принята. Спасибо!",
	AcceptedNew:       "Ваша тема «%s» добавлена. Спасибо!",
	AcceptedReport:    "Ошибку в «%s» исправили. Спасибо, что сообщили!",
	AcceptedCount:     "Принятых правок: %d.",
	RankUp:            "%s!",
	RankUpPrefix:      "Теперь вы ",
	RejectedEdit:      "Правку в «%s» не приняли.",
	RejectedNew:       "Тему «%s» не добавили.",
	RejectedReport:    "Сообщение об ошибке в «%s» закрыли без исправлений.",
	ReviewComment:     "Комментарий: «%s»",

	KeySubjects: "Предметы",
	KeyPinned:   "Закреплённые",
	KeyAccount:  "Аккаунт",
	KeyOld:      [3]string{"📚 Предметы", "📌 Закреплённые", "👤 Аккаунт"},
	Close:       "Закрыть",

	Pin:         "Закрепить",
	Unpin:       "Открепить",
	PinDone:     "Закреплено",
	UnpinDone:   "Откреплено",
	PinLimit:    "Можно закрепить не больше %d. Открепите что-нибудь в «Закреплённых».",
	PinnedTitle: "Закреплённые",
	PinnedEmpty: "Здесь будут темы, которые вы закрепите кнопкой «Закрепить».",

	AccountRank:     "Ранг: %s",
	AccountNext:     "До следующего ранга: ещё %d %s",
	AccountTop:      "Это высший ранг",
	AccountAccepted: "Принято правок: %d",
	AccountOpen:     "На рассмотрении: %d",
	AccountAdmin:    "Уровень прав: admin",
	Edits:           [3]string{"правка", "правки", "правок"},
	History:         "Список правок",
	HistoryPending:  "На рассмотрении",
	HistoryAccepted: "Приняты",
	HistoryRejected: "Отклонены",
	HistoryEmpty:    "Пока нет",
	HistoryItem:     "%s — %s, %s",
	KindEditLabel:   "правка",
	KindTopicLabel:  "новая тема",
	KindNoteLabel:   "новая запись",
	KindReportLabel: "ошибка",

	AccessNeeded:      "Чтобы предлагать правки, нужно разрешение. Отправить запрос?",
	SendRequest:       "Отправить запрос",
	AccessRequested:   "Запрос отправлен. Я напишу, когда его рассмотрят.",
	AccessPending:     "Запрос уже отправлен, ждём ответа.",
	AccessDeniedUntil: "Запрос отклонён. Повторить можно с %s.",
	Understood:        "Понятно",
	AccessNoAdmins:    "Сейчас правки не принимаются.",
	AccessHave:        "У вас уже есть доступ.",
	AdminRequest:      "Запрос на право предлагать правки\n%s\nПринято правок: %d · сообщений об ошибках: %d",
	Allow:             "Разрешить",
	Deny:              "Отклонить",
	AdminAllowed:      "Разрешено — %s",
	AdminDenied:       "Отклонено — %s",
	AdminDecided:      "Уже решено: %s",
	AccessGranted:     "Теперь вы можете предлагать правки.",
	AccessDenied:      "Запрос отклонён. Повторить можно через 7 дней.",
	AccessRevoked:     "Право предлагать правки отозвано.",
	AccessGone:        "Доступ отозван.",
	AccessList:        "Доступ есть у %d %s:",
	AccessListEmpty:   "Доступа пока нет ни у кого.",
	AccessListLine:    "• %s — с %s, разрешил(а) %s",
	AccessAdmins:      "Администраторы: %s",
	RevokeUsage:       "Напишите так: /revoke 123456 или /revoke @username",
	RevokeDone:        "Доступ отозван: %s.",
	RevokeUnknown:     "Не нашёл такого пользователя.",
	RevokeNoAccess:    "У этого пользователя нет доступа.",
	RevokeAdmin:       "Это администратор: доступ администраторов задаётся в настройках бота.",

	EmojiStep:          "Шаг %d из %d. %s",
	EmojiWhereSubjects: "Кнопка «Предметы» в нижней клавиатуре.",
	EmojiWherePinned:   "Кнопка «Закреплённые» в нижней клавиатуре.",
	EmojiWhereAccount:  "Кнопка «Аккаунт» в нижней клавиатуре.",
	EmojiWherePin:      "Кнопка «Закрепить» / «Открепить» под каждой темой.",
	EmojiWhereGreeting: "Приветствие после /start, сразу после «Привет, <имя>!».",
	EmojiHintGreeting:  "Что-нибудь приветственное, например машущая рука.",
	EmojiWhereSubject:  "Кнопка предмета «%s» в списке предметов.",
	EmojiWhereRank:     "Значок ранга %s (от %d %s): в «Аккаунте» и в поздравлении с новым рангом.",
	EmojiHintRank:      "Лучше, чтобы значки становились солиднее от ранга к рангу.",
	EmojiShortSubjects: "Предметы",
	EmojiShortPinned:   "Закреплённые",
	EmojiShortAccount:  "Аккаунт",
	EmojiShortPin:      "Закрепить",
	EmojiShortGreeting: "Приветствие",
	EmojiShortSubject:  "Предмет «%s»",
	EmojiShortRank:     "Ранг %s",
	EmojiNow:           "Сейчас: ",
	EmojiNowNone:       "Сейчас: нет",
	EmojiAsk:           "Пришлите кастомный эмодзи из набора.",
	EmojiSkip:          "Пропустить",
	EmojiClear:         "Без эмодзи",
	EmojiNeedCustom:    "Нужен кастомный эмодзи из набора.",
	EmojiSaveQuestion:  "Сохранить?",
	Yes:                "Да",
	EmojiOther:         "Другой",
	EmojiCancelled:     "Отменено, ничего не изменилось.",
	EmojiSaved:         "Готово, сохранено:",
	EmojiNone:          "—",
	EmojiUnknownSlot:   "Нет такого места. Напишите /emoji_setup, чтобы пройти все по порядку, или /emoji_setup <место>, где место — одно из:\n%s",
	EmojiSaveFailed:    "Не удалось сохранить: %s",
	RanksTitle:         "Ранги:",
	AcceptedFrom:       [3]string{"принятой правки", "принятых правок", "принятых правок"},
	RanksLine:          "%s — от %d %s",
	RanksEdit:          "Значки рангов меняются через /emoji_setup. Названия и пороги пока меняются только в файле data/settings.yml на сервере — бот перечитает его сам.",

	CmdStart:      "Открыть каталог",
	CmdDownload:   "Скачать все материалы",
	CmdCancel:     "Отменить текущее действие",
	CmdAccess:     "Кто может предлагать правки",
	CmdRevoke:     "Отозвать доступ: /revoke <id или @username>",
	CmdEmojiSetup: "Настроить кастомные эмодзи",
	CmdRanks:      "Показать ранги",

	Months: [12]string{"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря"},
}
