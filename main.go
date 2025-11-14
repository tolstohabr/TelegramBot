// TODO: завтра надо:
// TODO: 4) на месте заглушки реализовать реальный выбор города
// TODO: и добавить считывание прогнозов погоды по всем городам
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Forecast struct {
	Daily struct {
		Data []struct {
			Date        string `json:"day"`
			Summary     string `json:"summary"`
			Temperature string `json:"temperature"`
		} `json:"data"`
	} `json:"daily"`
}

// запрашиваю и возвращаю прогноз погоды
func getForecast() Forecast {
	apiKey := os.Getenv("API_KEY")

	lat := 55.7558
	lon := 37.6173

	url := fmt.Sprintf(
		"https://www.meteosource.com/api/v1/free/point?lat=%f&lon=%f&sections=daily&timezone=auto&language=en&key=%s",
		lat, lon, apiKey,
	)

	r, _ := http.Get(url)
	defer r.Body.Close()

	var f Forecast
	json.NewDecoder(r.Body).Decode(&f)
	return f
}

// отправляю сообщение в Telegram
func sendTelegramMessage(botToken, chatID, text string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	body := map[string]string{"chat_id": chatID, "text": text}
	jsonBody, _ := json.Marshal(body)
	http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
}

// преобразую прогноз в map[дата]summary
func forecastToMap(f Forecast) map[string]string {
	m := make(map[string]string)
	for _, d := range f.Daily.Data {
		m[d.Date] = d.Summary
	}
	return m
}

//TODO: dfdfd

type Update struct {
	UpdateID int `json:"update_id"`
	Message  *struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
	CallbackQuery *struct {
		ID      string `json:"id"`
		Data    string `json:"data"`
		Message struct {
			MessageID int `json:"message_id"`
		} `json:"message"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
	} `json:"callback_query"`
}

// получение обновлений
func getUpdates(botToken string, offset int) ([]Update, int) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d", botToken, offset)
	r, _ := http.Get(url)
	defer r.Body.Close()

	var result struct {
		Result []Update `json:"result"`
	}
	json.NewDecoder(r.Body).Decode(&result)

	newOffset := offset
	for _, u := range result.Result {
		if u.UpdateID >= newOffset {
			newOffset = u.UpdateID + 1
		}
	}
	return result.Result, newOffset
}

// отправка обычного меню (кнопки под полем ввода)
func sendMainMenu(botToken string, chatID int64) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	keyboard := map[string]interface{}{
		"keyboard": [][]map[string]string{
			{
				{"text": "Подписаться ✅"},
				{"text": "Отписаться ❌"},
			},
		},
		"resize_keyboard":   true,
		"is_persistent":     true,
		"one_time_keyboard": false,
	}

	body := map[string]interface{}{
		"chat_id":      chatID,
		"text":         "📋 Меню управления подпиской:",
		"reply_markup": keyboard,
	}

	jsonBody, _ := json.Marshal(body)
	http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
}

func sendCitySelection(botToken string, chatID int64) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	keyboard := map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "Москва", "callback_data": "Москва"},
			},
			{
				{"text": "Казань", "callback_data": "Казань"},
				{"text": "Санкт-Петербург", "callback_data": "Санкт-Петербург"},
			},
			{
				{"text": "Новосибирск", "callback_data": "Новосибирск"},
				{"text": "Екатеринбург", "callback_data": "Екатеринбург"},
			},
		},
	}

	body := map[string]interface{}{
		"chat_id":      chatID,
		"text":         "🏙️ Выберите город для подписки:",
		"reply_markup": keyboard,
	}

	jsonBody, _ := json.Marshal(body)
	http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
}

func answerCallbackQuery(botToken, callbackID string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", botToken)
	body := map[string]string{"callback_query_id": callbackID}
	jsonBody, _ := json.Marshal(body)
	http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
}

func removeInlineKeyboard(botToken string, chatID int64, messageID int) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageReplyMarkup", botToken)
	body := map[string]interface{}{
		"chat_id":      chatID,
		"message_id":   messageID,
		"reply_markup": map[string]interface{}{},
	}
	jsonBody, _ := json.Marshal(body)
	http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
}

func main() {
	godotenv.Load()
	botToken := os.Getenv("BOT_TOKEN")

	forecastCh := make(chan Forecast)
	notifyCh := make(chan string)

	// 1я горутина: получает прогноз
	go func() {
		for {
			f := getForecast()

			// удаляю из summary температуру
			for i := range f.Daily.Data {
				s := f.Daily.Data[i].Summary
				if idx := strings.Index(s, "Temperature"); idx != -1 {
					f.Daily.Data[i].Summary = strings.TrimSpace(s[:idx])
				}
			}
			fmt.Println("отправляю сообщение 2ой горутине")
			forecastCh <- f
			time.Sleep(10 * time.Minute)
		}
	}()

	// 2я горутина: ищет изменения в прогнозе
	go func() {
		var lastSummary map[string]string
		for f := range forecastCh {
			currentSummary := forecastToMap(f)

			changed := false
			changesText := "изменения в прогнозе погоды:\n"

			dates := make([]string, 0, len(currentSummary))
			for date := range currentSummary {
				dates = append(dates, date)
			}
			sort.Strings(dates)

			for _, date := range dates {
				summary := currentSummary[date]
				if lastSummary == nil || lastSummary[date] != summary {
					changed = true
					changesText += fmt.Sprintf("%s: %s\n", date, summary)
				}
			}

			if changed {
				fmt.Println("изменения в Summary обнаружены")
				fmt.Println("отправляю сообщение 3ей горутине")
				fmt.Println(changesText)
				lastSummary = currentSummary
				notifyCh <- changesText
			} else {
				fmt.Println("изменений в Summary нет")
			}
		}
	}()

	//TODO: dfdf

	subscribers := make(map[int64]bool)

	// горутина для приёма телеграм-команд
	go func() {
		offset := 0
		for {
			updates, newOffset := getUpdates(botToken, offset)
			offset = newOffset

			for _, update := range updates {
				//обычное
				if update.Message != nil {
					chatID := update.Message.Chat.ID
					text := update.Message.Text

					switch text {
					case "/start":
						sendMainMenu(botToken, chatID)

					case "Подписаться ✅":
						sendCitySelection(botToken, chatID)

					case "Отписаться ❌":
						delete(subscribers, chatID)
						sendTelegramMessage(botToken, fmt.Sprint(chatID), "❌ Вы отписались от уведомлений о погоде")
					}
				}

				//выбор города
				if update.CallbackQuery != nil {
					callback := update.CallbackQuery
					chatID := callback.From.ID
					city := callback.Data

					//TODO: подписываем пользователя (пока без фильтра по городу)
					subscribers[chatID] = true

					removeInlineKeyboard(botToken, chatID, callback.Message.MessageID)
					answerCallbackQuery(botToken, callback.ID)
					sendTelegramMessage(botToken, fmt.Sprint(chatID),
						fmt.Sprintf("✅ Подписка оформлена! Город: %s", city))
				}

			}

			time.Sleep(2 * time.Second)
		}
	}()

	// 3я горутина: рассылка всем подписчикам
	go func() {
		for msg := range notifyCh {
			for chatID := range subscribers {
				sendTelegramMessage(botToken, fmt.Sprint(chatID), msg)
			}
		}
	}()

	// Горутина для теста подписки
	go func() {
		for {
			for chatID := range subscribers {
				sendTelegramMessage(botToken, fmt.Sprint(chatID), "Подписан")
			}
			time.Sleep(5 * time.Second)
		}
	}()

	select {}
}
