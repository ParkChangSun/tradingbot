package notifier

import (
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramNotifier struct {
	Token  string
	ChatID int64
	Bot    *tgbotapi.BotAPI
}

func NewTelegramNotifier(token string, chatID int64) (*TelegramNotifier, error) {
	if token == "" || chatID == 0 {
		return &TelegramNotifier{Token: token, ChatID: chatID}, nil // 설정이 없는 경우 무시
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	return &TelegramNotifier{
		Token:  token,
		ChatID: chatID,
		Bot:    bot,
	}, nil
}

func (n *TelegramNotifier) Notify(message string) {
	if n.Bot == nil {
		fmt.Printf("[NOTIFY-LOG]: %s\n", message)
		return
	}

	msg := tgbotapi.NewMessage(n.ChatID, message)
	_, err := n.Bot.Send(msg)
	if err != nil {
		log.Printf("텔레그램 알림 전송 실패: %v\n", err)
	}
}
