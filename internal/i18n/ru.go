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
	DownloadMD     string
	NotFound       string
	TooLarge       string
	SendFailed     string
	UnknownCommand string

	// Error reports. Users are schoolchildren and teachers: no file names,
	// Markdown or LaTeX here, and every step offers «Отмена».
	ReportDisabled      string
	Continued           string // %s: note title
	ArchiveName         string // %s: date
	ReportError         string
	Cancel              string
	Send                string
	OpenTopic           string
	ReportPrompt        string
	GradeReportPrompt   string // %s: subject label, %s: grade title
	ReportReceived      string
	OnlyTextAndImages   string
	ImageTooLarge       string
	TooManyImages       string // %d: limit
	Expired             string
	TooOften            string
	TooManyOpen         string // %d: open reports
	Submitting          string
	SubmittedReport     string
	SubmitFailed        string
	AcceptedReport      string // %s: note title
	AcceptedGradeReport string // %s: «subject · grade»
	AcceptedCount       string // %d: accepted reports
	RankUp              string // %s: rank (after its emoji, if any)
	RankUpPrefix        string
	RejectedReport      string // %s: note title
	RejectedGradeReport string // %s: «subject · grade»
	ReviewComment       string // %s: comment
	// Outcomes of edits and new topics suggested in earlier versions,
	// whose pull requests may still be open.
	AcceptedEdit string // %s: topic title
	AcceptedNew  string // %s: topic title
	RejectedEdit string // %s: topic title
	RejectedNew  string // %s: topic title

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

	// Account and the list of reports, sent as rich messages (Markdown).
	AccountRank     string // %s: rank
	AccountNext     string // %d: accepted reports left, %s: Reports in the right form
	AccountTop      string
	AccountNextRank string // %s: the next rank
	AccountAccepted string // %d: accepted reports
	AccountOpen     string // %d: open reports
	AccountAdmin    string
	Reports         [3]string // «принятое сообщение», «принятых сообщения», «принятых сообщений»
	History         string
	HistoryPending  string
	HistoryAccepted string
	HistoryRejected string
	HistoryEmpty    string
	HistoryItem     string // %s: title, %s: kind, %s: date
	KindReportLabel string
	KindGradeLabel  string
	Top             string
	TopTitle        string
	TopEmpty        string
	TopYou          string // %d: place, %d: accepted reports, %s: Reports in the right form
	TopYouNone      string
	// Kinds of earlier versions, still in the history.
	KindEditLabel  string
	KindTopicLabel string
	KindNoteLabel  string

	// /emoji_setup: one step per place a custom emoji can go.
	EmojiStep          string // %d: step, %d: steps, %s: where
	EmojiWhereSubjects string
	EmojiWherePinned   string
	EmojiWhereAccount  string
	EmojiWherePin      string
	EmojiWhereGreeting string
	EmojiHintGreeting  string
	EmojiWhereSubject  string // %s: subject
	EmojiWhereRank     string // %s: rank, %d: accepted reports it starts at, %s: AcceptedFrom form
	EmojiHintRank      string
	EmojiWhereMedal    string // %d: place
	EmojiHintMedal     string // %d: place
	EmojiShortMedal    string // %d: place
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
	EmojiSaved         string
	EmojiNone          string
	EmojiUnknownSlot   string // %s: the slots
	EmojiSaveFailed    string // %s: error
	RanksTitle         string
	RanksLine          string    // %s: rank, %d: accepted reports, %s: AcceptedFrom form
	AcceptedFrom       [3]string // «от 1 принятого сообщения», «от 2 принятых сообщений»…
	RanksEdit          string

	// The command menu.
	CmdStart      string
	CmdDownload   string
	CmdCancel     string
	CmdEmojiSetup string
	CmdRanks      string
	CmdTop        string

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
	DownloadMD:     "Скачать",
	NotFound:       "Тема не найдена. Возможно, каталог обновился.",
	TooLarge:       "Эта тема слишком большая для одного сообщения. Нажмите «Скачать», чтобы получить её файлом.",
	SendFailed:     "Не удалось отправить тему. Попробуйте позже.",
	UnknownCommand: "Не понимаю. Нажмите /start, чтобы открыть каталог.",

	ReportDisabled:      "Сообщать об ошибках пока нельзя.",
	Continued:           "%s (продолжение)",
	ArchiveName:         "Теракоя %s.zip",
	ReportError:         "Сообщить об ошибке",
	Cancel:              "Отмена",
	Send:                "Отправить",
	OpenTopic:           "Открыть тему",
	ReportPrompt:        "Опишите своими словами, что не так. Например: «в теореме опечатка, должно быть 180°». Можно приложить фото или скриншот.",
	GradeReportPrompt:   "%s · %s. Опишите, чего здесь не хватает или что не так. Например: «нет темы про подобные треугольники». Можно приложить фото или скриншот.",
	ReportReceived:      "Понял! Можно добавить ещё фото или сразу отправить.",
	OnlyTextAndImages:   "Я понимаю только текст и картинки.",
	ImageTooLarge:       "Эта картинка слишком большая — пришлите поменьше.",
	TooManyImages:       "Больше %d картинок прислать нельзя.",
	Expired:             "Время вышло — начните заново кнопкой «Сообщить об ошибке».",
	TooOften:            "Подождите минутку перед следующим сообщением.",
	TooManyOpen:         "Ваших сообщений на проверке: %d. Подождите, пока мы их проверим, и возвращайтесь.",
	Submitting:          "Отправляю…",
	SubmittedReport:     "Спасибо! Мы посмотрим и исправим. Я напишу, когда это случится.",
	SubmitFailed:        "Не получилось отправить. Попробуйте ещё раз чуть позже.",
	AcceptedReport:      "Ошибку в «%s» исправили. Спасибо, что сообщили!",
	AcceptedGradeReport: "Ваше сообщение про «%s» учли. Спасибо!",
	AcceptedCount:       "Принято сообщений: %d.",
	RankUp:              "%s!",
	RankUpPrefix:        "Теперь вы ",
	RejectedReport:      "Сообщение об ошибке в «%s» закрыли без исправлений.",
	RejectedGradeReport: "Сообщение про «%s» закрыли без изменений.",
	ReviewComment:       "Комментарий: «%s»",
	AcceptedEdit:        "Ваша правка в «%s» принята. Спасибо!",
	AcceptedNew:         "Ваша тема «%s» добавлена. Спасибо!",
	RejectedEdit:        "Правку в «%s» не приняли.",
	RejectedNew:         "Тему «%s» не добавили.",

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
	AccountNextRank: "Следующий ранг: %s",
	AccountAccepted: "Принято сообщений: %d",
	AccountOpen:     "На рассмотрении: %d",
	AccountAdmin:    "Уровень прав: admin",
	Reports:         [3]string{"принятое сообщение", "принятых сообщения", "принятых сообщений"},
	History:         "Мои сообщения",
	HistoryPending:  "На рассмотрении",
	HistoryAccepted: "Приняты",
	HistoryRejected: "Отклонены",
	HistoryEmpty:    "Пока нет",
	HistoryItem:     "%s — %s, %s",
	KindReportLabel: "ошибка",
	KindGradeLabel:  "не хватает материала",
	Top:             "Топ",
	TopTitle:        "Топ участников",
	TopEmpty:        "Пока здесь никого нет. Сюда попадают те, чьи сообщения об ошибках приняли.",
	TopYou:          "Ваше место: %d — %d %s",
	TopYouNone:      "Вас пока нет в топе: сообщите об ошибке, и когда её примут, вы сюда попадёте.",
	KindEditLabel:   "правка",
	KindTopicLabel:  "новая тема",
	KindNoteLabel:   "новая запись",

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
	EmojiWhereMedal:    "Медаль %d места в /top — вместо номера места.",
	EmojiHintMedal:     "%d место",
	EmojiShortMedal:    "Топ, %d место",
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
	EmojiSaved:         "Готово, сохранено:",
	EmojiNone:          "—",
	EmojiUnknownSlot:   "Нет такого места. Напишите /emoji_setup, чтобы пройти все по порядку, или /emoji_setup <место>, где место — одно из:\n%s",
	EmojiSaveFailed:    "Не удалось сохранить: %s",
	RanksTitle:         "Ранги:",
	AcceptedFrom:       [3]string{"принятого сообщения", "принятых сообщений", "принятых сообщений"},
	RanksLine:          "%s — от %d %s",
	RanksEdit:          "Значки рангов меняются через /emoji_setup. Названия и пороги пока меняются только в файле data/settings.yml на сервере — бот перечитает его сам.",

	CmdStart:      "Открыть каталог",
	CmdDownload:   "Скачать все материалы",
	CmdCancel:     "Отменить текущее действие",
	CmdEmojiSetup: "Настроить кастомные эмодзи",
	CmdRanks:      "Показать ранги",
	CmdTop:        "Топ участников",

	Months: [12]string{"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря"},
}
