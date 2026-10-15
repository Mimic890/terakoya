// Package i18n holds every user-facing string of the bot.
package i18n

// Strings is the set of UI texts for one language.
type Strings struct {
	Greeting       string // %s: user's first name
	RankLine       string // %s: rank title, %d: approved edits
	ChooseSubject  string
	NoSubjects     string
	ChooseGrade    string // %s: subject label
	ChooseTopic    string // %s: subject label, %s: grade title
	NoTopics       string
	GradeTitle     string // %s: grade directory name
	Back           string
	Home           string
	SuggestEdit    string
	AddTopic       string
	DownloadMD     string
	ComingSoon     string
	NotFound       string
	TooLarge       string
	SendFailed     string
	UnknownCommand string
}

// RU is the Russian UI.
var RU = Strings{
	Greeting:       "Привет, %s! Это Теракоя — школьный справочник, который пишут сами ученики.",
	RankLine:       "Ваш ранг: %s · принятых правок: %d",
	ChooseSubject:  "Выберите предмет:",
	NoSubjects:     "Каталог пока пуст.",
	ChooseGrade:    "%s\n\nВыберите класс:",
	ChooseTopic:    "%s · %s\n\nВыберите тему:",
	NoTopics:       "В этом классе пока нет тем.",
	GradeTitle:     "%s класс",
	Back:           "« Назад",
	Home:           "В начало",
	SuggestEdit:    "Предложить правку",
	AddTopic:       "Добавить тему",
	DownloadMD:     "Скачать .md",
	ComingSoon:     "Скоро будет доступно.",
	NotFound:       "Тема не найдена. Возможно, каталог обновился.",
	TooLarge:       "Эта тема слишком большая для одного сообщения. Скачайте её как .md.",
	SendFailed:     "Не удалось отправить тему. Попробуйте позже.",
	UnknownCommand: "Не понимаю. Нажмите /start, чтобы открыть каталог.",
}
