// Package i18n holds every user-facing string of the bot.
package i18n

// Strings is the set of UI texts for one language.
type Strings struct {
	Greeting       string // %s: user's first name
	GreetingText   string // on the next line, after the greeting and its emoji
	ChooseSubject  string
	SubjectsTitle  string
	NoGrades       string
	GradeForms     [3]string // «1 класс», «2 класса», «5 классов»
	TopicForms     [3]string // «1 тема», «2 темы», «5 тем»
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
	SubjectReportPrompt string // %s: subject
	ReportReceived      string
	OnlyTextAndImages   string
	ImageTooLarge       string
	TooManyImages       string // %d: limit
	ReportTooLong       string // %d: limit in characters
	// A picture that turned out not to be a PNG or JPEG; %d: its number.
	NotAnImage          string
	NotAnImageNoneLeft  string
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
	KeySettings string
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
	Reports         [3]string // «принятая правка», «принятые правки», «принятых правок»
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
	EmojiShortSettings string
	EmojiWhereSettings string
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
	RanksButton        string
	RanksLine          string // %s: rank, %d: accepted reports, %s: AcceptedFrom form
	RanksHidden        string // %d: accepted reports, %s: AcceptedFrom form
	RanksYours         string
	AcceptedFrom       [3]string // «от 1 принятой правки», «от 2 принятых правок»…
	RanksEdit          string

	// /broadcast.
	BroadcastAsk           string
	BroadcastOnlyText      string
	BroadcastOnePhoto      string
	BroadcastPreviewFailed string
	BroadcastConfirm       string // %d: recipients
	BroadcastYes           string
	BroadcastEdit          string
	No                     string
	BroadcastProgress      string // %d: done, %d: recipients
	BroadcastDone          string // %d: sent, %d: blocked, %d: failed
	BroadcastBusy          string

	// «Настройки» and nicknames.
	SettingsTitle    string
	SettingsNick     string // %s: the name the user is addressed by
	SettingsTop      string // %s: SettingsYes or SettingsNo
	SettingsYes      string
	SettingsNo       string
	NickButton       string
	TopShown         string // the toggle while shown, with a check mark after it
	TopHidden        string
	NickTitle        string
	NickNowTelegram  string // %s: the Telegram first name
	NickNowOwn       string // %s: the nickname
	NickWhere        string
	NickFromTelegram string
	NickOwn          string
	NickAsk          string
	NickAskNew       string // %s: the current nickname
	NickAgain        string
	NickLength       string // %d: min, %d: max
	NickChars        string
	NickSpaces       string
	NickBanned       string
	NickReserved     string
	NickTaken        string
	NickResetUsage   string
	NickResetUnknown string
	NickResetNone    string // %s: user
	NickResetDone    string // %s: old nickname, %s: the name now
	NickResetNotice  string

	// /admin.
	AdminTitle        string
	AdminSummary      string // %d users, %d active 24h, %d open, %s commit, %s age, %s uptime
	AdminUsers        string
	AdminStats        string
	AdminServer       string
	AdminSync         string
	AdminBroadcast    string
	AdminUsersText    string // %d all, %d new today, %d new 7d, %d active 24h, %d active 7d, %d blocked
	AdminFound        string // %s query, %d found
	AdminNobody       string
	AdminSearch       string
	AdminSearchClear  string
	AdminSearchAsk    string
	AdminCard         string // name, ids line, nickname, rank, accepted, open, rejected, first seen, last seen, top, blocked
	AdminCardID       string // %d
	AdminCardUserID   string // %s @username, %d
	AdminNone         string
	AdminNoData       string
	AdminShown        string
	AdminHidden       string
	AdminResetNick    string
	AdminResetAsk     string // %s nickname, %s user
	AdminResetYes     string
	AdminResetDone    string
	AdminRefresh      string
	AdminComputing    string
	AdminSyncing      string
	AdminSyncFailed   string // %s
	AdminSyncNone     string // %s commit
	AdminSyncDone     string // %s → %s, %d → %d notes, %d errors, %d warnings
	AdminServerText   string // see adminServerText
	AdminServerFailed string // %s
	AdminLastSync     string // %s time, %s outcome
	AdminSyncNever    string
	AdminBackup       string // %s time, %s size, %d snapshots
	AdminBackupFailed string // %s time, %s error
	AdminBackupNone   string
	AdminSyncSame     string
	AdminAgo          string // %s
	AdminStatsTitle   string
	AdminReportsTitle string
	AdminReportsLine  string // %s period, %d open, %d accepted, %d rejected
	AdminAllTime      string
	AdminWeek         string
	AdminTopWeek      string
	AdminTopAll       string
	AdminNoteGone     string // %s id
	AdminNoOpens      string
	AdminBroadcasts   string // %d
	AdminLastBcast    string // %s date, %d recipients, %d sent, %d blocked, %d failed
	AdminDays         string // %d d, %d h
	AdminHours        string // %d h, %d min
	AdminMinutes      string // %d
	AdminSeconds      string // %d

	// The command menu.
	CmdStart      string
	CmdDownload   string
	CmdCancel     string
	CmdEmojiSetup string
	CmdRanks      string
	CmdTop        string
	CmdBroadcast  string
	CmdNickReset  string
	CmdAdmin      string

	// Months in the genitive case, for dates such as «25 октября».
	Months [12]string
}

// RU is the Russian UI.
var RU = Strings{
	Greeting: "Привет, %s!",
	GreetingText: "Это terakoya — школьный справочник: теоремы, аксиомы, задачи и доказательства по классам.\n" +
		"Нашли ошибку или чего-то не хватает? Сообщите — за принятые правки растёт ранг.",
	ChooseSubject:  "Выберите предмет:",
	SubjectsTitle:  "Предметы",
	NoGrades:       "Здесь пока нет классов.",
	GradeForms:     [3]string{"класс", "класса", "классов"},
	TopicForms:     [3]string{"тема", "темы", "тем"},
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
	SubjectReportPrompt: "%s. Опишите, что не так или чего не хватает. Например: «нет 11 класса». Можно приложить фото или скриншот.",
	ReportReceived:      "Понял! Можно добавить ещё фото или сразу отправить.",
	OnlyTextAndImages:   "Я понимаю только текст и картинки.",
	ImageTooLarge:       "Эта картинка слишком большая — пришлите поменьше.",
	TooManyImages:       "Больше %d картинок прислать нельзя.",
	ReportTooLong:       "Описание не может быть длиннее %d символов, поэтому это сообщение не добавлено. Сократите его и пришлите ещё раз.",
	NotAnImage:          "Картинка №%d оказалась не PNG и не JPEG, поэтому её убрали. Отправьте сообщение без неё или пришлите другую картинку.",
	NotAnImageNoneLeft:  "Картинка №%d оказалась не PNG и не JPEG, поэтому её убрали. Опишите ошибку словами или пришлите другую картинку.",
	Expired:             "Время вышло — начните заново кнопкой «Сообщить об ошибке».",
	TooOften:            "Подождите минутку перед следующим сообщением.",
	TooManyOpen:         "Ваших правок на проверке: %d. Подождите, пока мы их проверим, и возвращайтесь.",
	Submitting:          "Отправляю…",
	SubmittedReport:     "Спасибо! Мы посмотрим и исправим. Я напишу, когда это случится.",
	SubmitFailed:        "Не получилось отправить. Попробуйте ещё раз чуть позже.",
	AcceptedReport:      "Ошибку в «%s» исправили. Спасибо, что сообщили!",
	AcceptedGradeReport: "Ваше сообщение про «%s» учли. Спасибо!",
	AcceptedCount:       "Принято правок: %d.",
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
	KeySettings: "Настройки",
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
	AccountAccepted: "Принято правок: %d",
	AccountOpen:     "На рассмотрении: %d",
	AccountAdmin:    "Уровень прав: admin",
	Reports:         [3]string{"принятая правка", "принятые правки", "принятых правок"},
	History:         "Мои правки",
	HistoryPending:  "На рассмотрении",
	HistoryAccepted: "Приняты",
	HistoryRejected: "Отклонены",
	HistoryEmpty:    "Пока нет",
	HistoryItem:     "%s — %s, %s",
	KindReportLabel: "ошибка",
	KindGradeLabel:  "не хватает материала",
	Top:             "Топ",
	TopTitle:        "Топ участников",
	TopEmpty:        "Пока здесь никого нет. Сюда попадают те, чьи правки приняли.",
	TopYou:          "Ваше место: %d — %d %s",
	TopYouNone:      "Вас пока нет в топе: вы попадёте сюда, когда примут вашу первую правку.",
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
	EmojiShortSettings: "Настройки",
	EmojiWhereSettings: "Кнопка «Настройки» в нижней клавиатуре.",
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
	RanksTitle:         "Ранги",
	RanksButton:        "Ранги",
	AcceptedFrom:       [3]string{"принятой правки", "принятых правок", "принятых правок"},
	RanksLine:          "%s — от %d %s",
	RanksHidden:        "??? — от %d %s",
	RanksYours:         " · ваш ранг",
	RanksEdit:          "Значки рангов меняются через /emoji_setup. Названия и пороги пока меняются только в файле data/settings.yml на сервере — бот перечитает его сам.",

	BroadcastAsk:           "Пришлите сообщение для рассылки.",
	BroadcastOnlyText:      "Для рассылки подойдёт текст, можно с одной фотографией.",
	BroadcastOnePhoto:      "В рассылке может быть только одна фотография — взял первую.",
	BroadcastPreviewFailed: "Telegram не принял это сообщение. Пришлите другое.",
	BroadcastConfirm:       "Отправить всем? Получателей: %d",
	BroadcastYes:           "Да, отправить",
	BroadcastEdit:          "Изменить",
	No:                     "Нет",
	BroadcastProgress:      "Рассылка: отправлено %d из %d…",
	BroadcastDone:          "Рассылка завершена. Доставлено: %d, заблокировали бота: %d, не доставлено: %d.",
	BroadcastBusy:          "Сейчас идёт другая рассылка. Когда она закончится, нажмите «Да, отправить» ещё раз.",

	SettingsTitle:    "Настройки",
	SettingsNick:     "Ник: %s",
	SettingsTop:      "Показывать меня в топе: %s",
	SettingsYes:      "да",
	SettingsNo:       "нет",
	NickButton:       "Ник",
	TopShown:         "Топ: показывать",
	TopHidden:        "Топ: скрыт",
	NickTitle:        "Ник",
	NickNowTelegram:  "Сейчас: %s (из Telegram)",
	NickNowOwn:       "Сейчас: %s (свой)",
	NickWhere:        "Ник видно в топе и в приветствии.",
	NickFromTelegram: "Из Telegram",
	NickOwn:          "Свой ник",
	NickAsk:          "Пришлите ник: от 3 до 20 символов — буквы, цифры, «_», «-» и пробелы.",
	NickAskNew:       "Сейчас: %s. Пришлите новый ник.",
	NickAgain:        "Пришлите другой.",
	NickLength:       "Ник должен быть от %d до %d символов.",
	NickChars:        "В нике могут быть только буквы, цифры, «_», «-» и пробелы.",
	NickSpaces:       "Пробел не может стоять в начале или в конце ника, и два пробела подряд тоже нельзя.",
	NickBanned:       "Такой ник использовать нельзя.",
	NickReserved:     "Такой ник можно спутать с администратором или ботом.",
	NickTaken:        "Этот ник уже занят.",
	NickResetUsage:   "Напишите так: /nick_reset 123456 или /nick_reset @username",
	NickResetUnknown: "Не нашёл такого пользователя.",
	NickResetNone:    "У %s нет своего ника.",
	NickResetDone:    "Ник «%s» сброшен, теперь это %s.",
	NickResetNotice:  "Ваш ник сброшен администратором.",

	AdminTitle:       "Админка",
	AdminSummary:     "Пользователей: %d, активны за 24 ч: %d\nПравок на рассмотрении: %d\nVault: %s, %s\nРаботает: %s",
	AdminUsers:       "Пользователи",
	AdminStats:       "Статистика",
	AdminServer:      "Сервер",
	AdminSync:        "Обновить vault",
	AdminBroadcast:   "Рассылка",
	AdminUsersText:   "Пользователи\n\nВсего: %d · новых сегодня: %d · за 7 дней: %d\nАктивны за 24 ч: %d · за 7 дней: %d\nЗаблокировали бота: %d",
	AdminFound:       "Поиск «%s»: найдено %d",
	AdminNobody:      "Никого не нашлось.",
	AdminSearch:      "Поиск",
	AdminSearchClear: "Сбросить поиск",
	AdminSearchAsk:   "Кого найти? Пришлите ник, @username, имя или Telegram ID.",
	AdminCard:        "%s\n%s\n\nНик: %s\nРанг: %s\nПравки: принято %d · на рассмотрении %d · отклонено %d\nВпервые: %s\nПоследний раз: %s\nВ топе: %s\nЗаблокировал бота: %s",
	AdminCardID:      "ID %d",
	AdminCardUserID:  "@%s · ID %d",
	AdminNone:        "нет",
	AdminNoData:      "нет данных",
	AdminShown:       "показан",
	AdminHidden:      "скрыт",
	AdminResetNick:   "Сбросить ник",
	AdminResetAsk:    "Сбросить ник «%s» у %s? Пользователь получит уведомление.",
	AdminResetYes:    "Да, сбросить",
	AdminResetDone:   "Ник сброшен",
	AdminRefresh:     "Обновить",
	AdminComputing:   "Считаю…",
	AdminSyncing:     "Обновляю…",
	AdminSyncFailed:  "Не удалось обновить vault: %s",
	AdminSyncNone:    "Изменений нет, коммит %s",
	AdminSyncDone:    "Vault обновлён: %s → %s\nЗаметок: %d → %d\nПроверка: ошибок %d, предупреждений %d",
	AdminServerText: "Сервер\n\n" +
		"Память: RSS %s, Go heap %s, горутин %d\n" +
		"Работает: %s · версия %s\n\n" +
		"Сеть контейнера: получено %s, отправлено %s\n" +
		"Telegram API сегодня: запросов %d, отправлено %s, получено %s\n" +
		"Telegram API всего: запросов %d, отправлено %s, получено %s\n\n" +
		"Vault на диске: %s (заметки %s, картинки %s, .git %s)\n" +
		"Данные: %s, база %s, file_id в кэше: %d\n\n" +
		"Vault: коммит %s (%s), заметок %d\n%s",
	AdminServerFailed: "Не удалось посчитать: %s",
	AdminLastSync:     "Последняя синхронизация: %s — %s",
	AdminSyncNever:    "Синхронизаций с запуска не было",
	AdminBackup:       "Резервная копия: %s, %s · всего копий: %d",
	AdminBackupFailed: "Резервная копия не удалась (%s): %s",
	AdminBackupNone:   "Резервных копий пока нет",
	AdminSyncSame:     "без изменений",
	AdminAgo:          "%s назад",
	AdminStatsTitle:   "Статистика",
	AdminReportsTitle: "Правки",
	AdminReportsLine:  "%s: на рассмотрении %d · принято %d · отклонено %d",
	AdminAllTime:      "Всего",
	AdminWeek:         "За 7 дней",
	AdminTopWeek:      "Чаще всего открывают — 7 дней",
	AdminTopAll:       "Чаще всего открывают — всё время",
	AdminNoteGone:     "%s (удалена)",
	AdminNoOpens:      "Пока нет",
	AdminBroadcasts:   "Рассылок: %d",
	AdminLastBcast:    "Последняя: %s — получателей %d, доставлено %d, заблокировали бота %d, не доставлено %d",
	AdminDays:         "%d д %d ч",
	AdminHours:        "%d ч %d мин",
	AdminMinutes:      "%d мин",
	AdminSeconds:      "%d с",

	CmdStart:      "Открыть каталог",
	CmdDownload:   "Скачать все материалы",
	CmdCancel:     "Отменить текущее действие",
	CmdEmojiSetup: "Настроить кастомные эмодзи",
	CmdRanks:      "Показать ранги",
	CmdTop:        "Топ участников",
	CmdBroadcast:  "Рассылка всем пользователям",
	CmdNickReset:  "Сбросить ник: /nick_reset <id или @username>",
	CmdAdmin:      "Админка",

	Months: [12]string{"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря"},
}
