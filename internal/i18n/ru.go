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

	SuggestDisabled  string
	SuggestEditIntro string // %s: article title
	SuggestNewIntro  string // %s: subject label, %s: grade title
	Cancel           string
	Cancelled        string
	NothingToCancel  string
	TooOften         string // %d: seconds to wait
	TooManyOpen      string // %d: open suggestions
	ImageSaved       string // %s, %s: file name
	ImageUnsupported string
	ImageTooLarge    string // %d: limit in MB
	TooManyImages    string // %d: limit
	FileTooLarge     string // %d: limit in KB
	Unchanged        string
	Submitting       string
	Submitted        string // %d: pull request number
	SubmittedIssues  string // %d: number of lint findings
	SubmitFailed     string
	Merged           string // %s: title, %d: merged count, %s: rank
	RankUp           string // %s: rank
	Closed           string // %s: title
	ReviewComment    string // %s: comment
	// Problems explains why a submission was rejected, by suggest.Code.
	Problems map[string]string
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
	DownloadMD:     "Скачать .md",
	NotFound:       "Тема не найдена. Возможно, каталог обновился.",
	TooLarge:       "Эта тема слишком большая для одного сообщения. Скачайте её как .md.",
	SendFailed:     "Не удалось отправить тему. Попробуйте позже.",
	UnknownCommand: "Не понимаю. Нажмите /start, чтобы открыть каталог.",

	SuggestDisabled: "Предложение правок пока не подключено.",
	SuggestEditIntro: "Текущий текст темы «%s» — в файле выше.\n\n" +
		"Отредактируйте его и пришлите обратно как документ .md или просто отправьте новый текст сообщением. " +
		"Если блока frontmatter в начале нет, он останется прежним.\n\n" +
		"Нужны новые картинки? Сначала пришлите их как файлы (png, jpg, webp) — я подскажу, как вставить их в текст.\n\n" +
		"Отменить: кнопка «Отмена» или /cancel.",
	SuggestNewIntro: "Новая тема: %s · %s.\n\n" +
		"Пришлите документ .md или текст сообщением. Первая строка станет названием темы, " +
		"если в начале нет блока frontmatter с полем title.\n\n" +
		"Нужны картинки? Сначала пришлите их как файлы (png, jpg, webp) — я подскажу, как вставить их в текст.\n\n" +
		"Отменить: кнопка «Отмена» или /cancel.",
	Cancel:           "Отмена",
	Cancelled:        "Отменено.",
	NothingToCancel:  "Нечего отменять.",
	TooOften:         "Слишком часто. Следующую правку можно предложить через %d с.",
	TooManyOpen:      "У вас уже %d правок на проверке. Дождитесь решения по ним.",
	ImageSaved:       "Картинка сохранена как assets/%s. Вставьте её в текст отдельной строкой:\n\n![описание](assets/%s)",
	ImageUnsupported: "Подходят только файлы .md и картинки png, jpg или webp.",
	ImageTooLarge:    "Картинка больше %d МБ.",
	TooManyImages:    "К одной правке можно приложить не больше %d картинок.",
	FileTooLarge:     "Файл больше %d КБ.",
	Unchanged:        "Текст не изменился — отправлять нечего.",
	Submitting:       "Отправляю правку…",
	Submitted:        "Спасибо! Правка отправлена на проверку (запрос #%d). Я напишу, когда её рассмотрят.",
	SubmittedIssues:  "Проверка нашла замечаний: %d. Проверяющий их увидит.",
	SubmitFailed:     "Не удалось отправить правку. Попробуйте позже.",
	Merged:           "Ваша правка «%s» принята! Принятых правок: %d. Ранг: %s.",
	RankUp:           "Новый ранг: %s! 🎉",
	Closed:           "Правка «%s» отклонена.",
	ReviewComment:    "Комментарий проверяющего:\n%s",
	Problems: map[string]string{
		"too_large":        "Файл больше %d КБ.",
		"not_utf8":         "Файл должен быть в кодировке UTF-8.",
		"empty":            "Текст пустой.",
		"no_frontmatter":   "В начале файла нужен блок frontmatter с полем title.",
		"open_frontmatter": "Блок frontmatter в начале файла не закрыт строкой ---.",
		"bad_frontmatter":  "Ошибка в блоке frontmatter: %s",
		"no_title":         "Не нашёл названия темы: нужно поле title во frontmatter или первая строка текста.",
		"bad_id":           "Недопустимый id %q: только латиница, цифры, _ и -, не длиннее %d символов.",
		"taken_id":         "id %q уже занят темой «%s».",
		"over_limit":       "Тема не поместится в одно сообщение (%s). Разделите её на части.",
		"bad_path":         "Недопустимый путь %q.",
	},
}
