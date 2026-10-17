// Package i18n holds every user-facing string of the bot.
package i18n

// Strings is the set of UI texts for one language.
type Strings struct {
	Greeting       string // %s: user's first name
	RankLine       string // %s: rank title, %d: approved edits
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
	RankUp             string // %s: rank
	RejectedEdit       string // %s: topic title
	RejectedNew        string // %s: topic title
	RejectedReport     string // %s: topic title
	ReviewComment      string // %s: comment
}

// RU is the Russian UI.
var RU = Strings{
	Greeting:       "Привет, %s! Это Теракоя — школьный справочник, который пишут сами ученики.",
	RankLine:       "Ваш ранг: %s · принятых правок: %d",
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
	RankUp:            "Теперь вы %s! 🎉",
	RejectedEdit:      "Правку в «%s» не приняли.",
	RejectedNew:       "Тему «%s» не добавили.",
	RejectedReport:    "Сообщение об ошибке в «%s» закрыли без исправлений.",
	ReviewComment:     "Комментарий: «%s»",
}
