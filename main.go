package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-co-op/gocron"
)

const (
	shopID    = "" // Ваш shopID
	secretKey = "" // Ваш secretKey
	apiURL    = "https://api.yookassa.ru/v3/payments"
)

// Payment представляет структуру платежа
type Payment struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Confirmation struct {
		Type            string `json:"type"`
		ReturnURL       string `json:"return_url"`
		ConfirmationURL string `json:"confirmation_url"` // Ссылка на оплату
	} `json:"confirmation"`
	Description string `json:"description"`
	Capture     bool   `json:"capture"`
	Test        bool   `json:"test"`
}

// createPayment создает платеж
func createPayment() (*Payment, error) {
	payload := map[string]interface{}{
		"amount": map[string]interface{}{
			"value":    "10.00",
			"currency": "RUB",
		},
		"confirmation": map[string]interface{}{
			"type":       "redirect",
			"return_url": "https://your-website.com/return", // Замените на реальный URL
		},
		"description": "Test payment",
		"capture":     true,
		"test":        true,
		"receipt": map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"description": "Тестовый товар",
					"quantity":    "1.00",
					"amount": map[string]interface{}{
						"value":    "10.00", // Сумма совпадает с amount
						"currency": "RUB",
					},
					"vat_code":        2, // 0% НДС
					"payment_mode":    "full_payment",
					"payment_subject": "commodity",
				},
			},
			"email": "user@example.com", // Укажите реальный email
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Используем фиксированный ключ для теста
	idempotencyKey := fmt.Sprintf("test-idempotency-%d", time.Now().UnixNano())
	req.SetBasicAuth(shopID, secretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotence-Key", idempotencyKey)

	// Отладочный вывод запроса
	log.Printf("Request method: %s, URL: %s", req.Method, req.URL)
	log.Printf("Request headers: %+v", req.Header)
	log.Printf("Request body: %s", string(body))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Логируем статус и заголовки ответа
	log.Printf("Response status: %s", resp.Status)
	log.Printf("Response headers: %+v", resp.Header)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, response: %s", resp.StatusCode, string(body))
	}

	var payment Payment
	if err := json.NewDecoder(resp.Body).Decode(&payment); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &payment, nil
}

// checkPaymentStatus проверяет статус платежа
func checkPaymentStatus(paymentID string) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/%s", apiURL, paymentID), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	req.SetBasicAuth(shopID, secretKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code: %d, response: %s", resp.StatusCode, string(body))
	}

	var payment Payment
	if err := json.NewDecoder(resp.Body).Decode(&payment); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	return payment.Status, nil
}

func main() {
	// Создание платежа
	payment, err := createPayment()
	if err != nil {
		log.Fatalf("Failed to create payment: %v", err)
	}

	fmt.Printf("Payment created with ID: %s, Status: %s\n", payment.ID, payment.Status)
	fmt.Printf("Payment URL: %s\n", payment.Confirmation.ConfirmationURL)

	// Инициализация планировщика
	scheduler := gocron.NewScheduler(time.UTC)

	// Флаг для остановки проверки
	done := make(chan bool)

	// Счетчик для ограничения времени проверки (10 минут = 600 секунд)
	maxChecks := 20 // 10 минут / 30 секунд = 20 проверок
	checkCount := 0

	// Планируем задачу проверки статуса каждые 30 секунд
	_, err = scheduler.Every(30).Seconds().Do(func() {
		checkCount++
		status, err := checkPaymentStatus(payment.ID)
		if err != nil {
			log.Printf("Error checking payment status: %v", err)
			return
		}

		fmt.Printf("Payment ID: %s, Status: %s\n", payment.ID, status)

		// Если платеж достиг финального статуса, останавливаем проверки
		if status == "succeeded" || status == "canceled" {
			fmt.Printf("Final status reached: %s. Stopping checks.\n", status)
			done <- true
			return
		}

		// Если прошло 10 минут, останавливаем проверки
		if checkCount >= maxChecks {
			fmt.Println("10 minutes elapsed. Stopping checks.")
			done <- true
		}
	})

	if err != nil {
		log.Fatalf("Failed to schedule job: %v", err)
	}

	// Запускаем планировщик асинхронно
	scheduler.StartAsync()

	// Ждем сигнала остановки
	<-done

	// Останавливаем планировщик
	scheduler.Stop()

	fmt.Println("Payment status checking completed.")
}
