package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Hand-TBN1/hand-backend/config"
	"github.com/Hand-TBN1/hand-backend/models"
	"github.com/Hand-TBN1/hand-backend/services"
	"github.com/Hand-TBN1/hand-backend/utilities"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type ClientMessage struct {
	Event string `json:"event"` // Event type (e.g., "find_match", "send_message")
	Data  string `json:"data"`  // Message content or event data
}

type ClientQueue struct {
	UserID string `json:"user_id"`
	Emote  int    `json:"emote"`
}

type ClientInServer struct {
	Ws    *websocket.Conn
	Match *models.ChatRoom
	Queue *ClientQueue
}

type TherapyMessageData struct {
	RoomID  string `json:"room_id"`
	Message string `json:"message"`
}

var (
	upgrader            = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	clients             = make(map[string]*ClientInServer)
	clientsM            sync.Mutex
	redisClient         *redis.Client
	db                  *gorm.DB
	subscriptionService *services.SubscriptionService
)

func loadEnvironment() {
	if err := godotenv.Load("../.env"); err != nil {
		log.Printf("Error loading .env file: %v", err)
	}
}

func initializeServices() {
	config.LoadEnv()
	db = config.NewPostgresql(&models.Subscription{}, &models.ChatMessage{}, &models.ChatRoom{})
	redisClient = config.NewRedis()
	subscriptionService = &services.SubscriptionService{DB: db}
}

func main() {
	loadEnvironment()
	initializeServices()
	http.HandleFunc("/ws", handleConnections)
	log.Println("WebSocket server starting on http://localhost:8000/ws")
	log.Fatal(http.ListenAndServe(":8000", nil))
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	log.Println("masuk")
	userID, ws, err := authenticateAndUpgrade(w, r)
	if err != nil {
		return // Error handled within function
	}
	defer ws.Close()

	client := &ClientInServer{Ws: ws}
	clientsM.Lock()
	clients[userID] = client
	clientsM.Unlock()

	listenForMessages(ws, userID)
	cleanupClient(userID)
}

func authenticateAndUpgrade(w http.ResponseWriter, r *http.Request) (string, *websocket.Conn, error) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Unauthorized - Token not provided", http.StatusUnauthorized)
		return "", nil, fmt.Errorf("no token provided")
	}

	claims, err := utilities.ValidateJWT(token)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", nil, err
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade WebSocket: %v", err)
		return "", nil, err
	}

	return claims.UserID, ws, nil
}

func listenForMessages(ws *websocket.Conn, userID string) {
	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			log.Printf("Error reading message: %v", err)
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				log.Printf("WebSocket closed normally for userID %s", userID)
				break // Stop the loop for normal closures
			} else {
				log.Printf("Recoverable error for userID %s: %v", userID, err)

				continue // Continue the loop for other types of errors
			}
		}

		handleClientMessage(message, userID)
	}
}

func handleClientMessage(message []byte, userID string) {
	var msg ClientMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Error unmarshalling message: %v", err)
		return
	}

	switch msg.Event {
	case "find_match":
		findMatch(userID, msg.Data)
	case "send_message":
		sendMessageToPartner(userID, msg.Data)
	case "check_match":
		checkMatchStatus(userID)
	case "end_match":
		endMatch(userID)
	case "cancel_find":
		cancelFind(userID)
	case "chat_therapy":
		therapistID := msg.Data // Assume Data contains therapistID for simplicity
		room, err := findOrCreateTherapyRoom(userID, therapistID)
		if err != nil {
			log.Printf("Error finding or creating therapy room: %v", err)
			return
		}
		sendEnterRoomEvent(userID, room.ID)

	case "therapy_message":
		// Assume Data contains JSON with roomID and message
		var therapyData TherapyMessageData
		if err := json.Unmarshal([]byte(msg.Data), &therapyData); err != nil {
			log.Printf("Error parsing therapy message details: %v", err)
			return
		}
		sendMessageToRoom(userID, therapyData.RoomID, therapyData.Message)
	default:
		log.Printf("Unknown event: %s", msg.Event)
	}
}

func findMatch(userID, data string) {
	var queueData ClientQueue
	if err := json.Unmarshal([]byte(data), &queueData); err != nil {
		log.Printf("Error parsing queue data: %v", err)
		return
	}

	ctx := context.Background()
	queueData.UserID = userID
	serializedQueueData, _ := json.Marshal(queueData)

	// Attempt to find a matching queue item
	matchedData, err := redisClient.SPop(ctx, "waitingQueue").Result()
	if err != nil || matchedData == "" {
		// No match found, add user to queue
		redisClient.SAdd(ctx, "waitingQueue", serializedQueueData)
		log.Printf("User %s added to waiting queue", userID)
		clients[userID].Queue = &queueData
		clients[userID].Ws.WriteMessage(websocket.TextMessage, []byte(`{"event": "in_queue", "data": ""}`))
		return
	}

	var matchedQueue ClientQueue
	json.Unmarshal([]byte(matchedData), &matchedQueue)

	if matchedQueue.Emote == queueData.Emote {
		createChatRoom(userID, matchedQueue.UserID)
	} else {
		// No suitable match found, re-add to the queue
		redisClient.SAdd(ctx, "waitingQueue", serializedQueueData)
		clients[userID].Queue = &queueData
		redisClient.SAdd(ctx, "waitingQueue", matchedData)
		clients[userID].Ws.WriteMessage(websocket.TextMessage, []byte(`{"event": "in_queue", "data": ""}`))
	}
}

func createChatRoom(firstUserID, secondUserID string) {
	roomID := uuid.New()
	newRoom := models.ChatRoom{
		ID:           roomID,
		FirstUserID:  uuid.MustParse(firstUserID),
		SecondUserID: uuid.MustParse(secondUserID),
		Type:         "anonymous",
		IsEnd:        false,
	}
	db.Create(&newRoom)
	clients[firstUserID].Queue = nil
	clients[secondUserID].Queue = nil
	clients[firstUserID].Match = &newRoom
	clients[secondUserID].Match = &newRoom
	notifyUsersOfMatch(firstUserID, secondUserID, roomID)
}

func notifyUsersOfMatch(firstUserID, secondUserID string, roomID uuid.UUID) {
	message := fmt.Sprintf(`{"event": "matched", "data": "%s"}`, roomID)
	clients[firstUserID].Ws.WriteMessage(websocket.TextMessage, []byte(message))
	clients[secondUserID].Ws.WriteMessage(websocket.TextMessage, []byte(message))
}

func sendMessageToPartner(userID, message string) {

	room, exists := findChatRoomForUser(userID)
	if !exists {
		return
	}

	senderUUID, err := uuid.Parse(userID)
	if err != nil {
		log.Printf("Invalid sender UUID: %v", err)
		return
	}

	if ok, reason := canSendChatMessage(userID); !ok {
		notifyLimitReached(userID, reason)
		return
	}

	chatMessage := models.ChatMessage{
		SenderID:       senderUUID,
		ChatRoomID:     room.ID,
		MessageContent: message,
		SentAt:         time.Now(),
	}

	if result := db.Create(&chatMessage); result.Error != nil {
		log.Printf("Failed to save chat message: %v", result.Error)
		return
	}

	data := map[string]interface{}{
		"event": "message",
		"data":  chatMessage,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error marshalling data: %v", err)
		return
	}
	partnerID := getPartnerID(room, userID)
	clients[userID].Ws.WriteMessage(websocket.TextMessage, jsonData)
	clients[partnerID].Ws.WriteMessage(websocket.TextMessage, jsonData)
}

func findChatRoomForUser(userID string) (*models.ChatRoom, bool) {
	var room models.ChatRoom
	// Add the filter for Type = 'anonymous' in the WHERE clause
	if err := db.Where("(first_user_id = ? OR second_user_id = ?) AND is_end = false AND type = ?", userID, userID, "anonymous").First(&room).Error; err != nil {
		return nil, false
	}
	return &room, true
}

func getPartnerID(room *models.ChatRoom, userID string) string {
	if room.FirstUserID.String() == userID {
		return room.SecondUserID.String()
	}
	return room.FirstUserID.String()
}

func cleanupClient(userID string) {
	clientsM.Lock()
	delete(clients, userID)
	clientsM.Unlock()
}

func checkMatchStatus(userID string) {

	ctx := context.Background()
	if clients[userID].Queue == nil {
		queueMembers, err := redisClient.SMembers(ctx, "waitingQueue").Result()
		if err != nil {
			log.Printf("Error retrieving Redis queue: %v", err)
			clients[userID].Ws.WriteMessage(websocket.TextMessage, []byte(`{"event": "error", "data": "Unable to check queue status"}`))
			return
		}

		foundInQueue := false
		var userQueue ClientQueue

		for _, queueData := range queueMembers {
			var queuedUser ClientQueue
			if err := json.Unmarshal([]byte(queueData), &queuedUser); err == nil && queuedUser.UserID == userID {
				userQueue = queuedUser
				foundInQueue = true
				break
			}
		}

		if foundInQueue {
			// User is found in the queue, update the ClientInServer.Queue
			clientsM.Lock()
			clients[userID].Queue = &userQueue
			clientsM.Unlock()
		}
	}

	if clients[userID].Match == nil {
		room, exists := findChatRoomForUser(userID)
		if exists {
			clientsM.Lock()
			clients[userID].Match = room
			clientsM.Unlock()
		}
	}
	clientsM.Lock()
	client := clients[userID]
	clientsM.Unlock()

	if client.Queue != nil {
		client.Ws.WriteMessage(websocket.TextMessage, []byte(`{"event": "in_queue", "data": ""}`))
	} else if client.Match != nil {
		client.Ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"event": "matched", "data": "%s"}`, client.Match.ID)))
	} else {
		client.Ws.WriteMessage(websocket.TextMessage, []byte(`{"event": "free", "data": ""}`))
	}
}

func cancelFind(userID string) {
	clientsM.Lock()
	client := clients[userID]
	if client == nil || client.Queue == nil {
		clientsM.Unlock()
		return // Either user is not connected or not in any queue
	}

	// Attempt to remove the user from the Redis queue
	ctx := context.Background()
	queueMembers, err := redisClient.SMembers(ctx, "waitingQueue").Result()
	if err != nil {
		log.Printf("Error retrieving Redis queue for cancellation: %v", err)
		client.Ws.WriteMessage(websocket.TextMessage, []byte(`{"event": "error", "data": "Queue access failed"}`))
		clientsM.Unlock()
		return
	}

	// Filter out this user's queue data
	updatedQueue := make([]interface{}, 0)
	for _, queueData := range queueMembers {
		var queuedUser ClientQueue
		if err := json.Unmarshal([]byte(queueData), &queuedUser); err == nil && queuedUser.UserID != userID {
			updatedQueue = append(updatedQueue, queueData)
		}
	}

	// Reset the waiting queue without this user
	redisClient.Del(ctx, "waitingQueue")
	if len(updatedQueue) > 0 {
		redisClient.SAdd(ctx, "waitingQueue", updatedQueue...)
	}

	// Reset the client's queue status
	client.Queue = nil
	client.Ws.WriteMessage(websocket.TextMessage, []byte(`{"event": "cancel_find", "data": ""}`))

	clientsM.Unlock()
}

func endMatch(userID string) {
	client, exists := clients[userID]
	if !exists || client.Match == nil {
		return // Either user is not connected, or there is no match to end
	}

	// Update the database to mark the chat room as ended
	var room models.ChatRoom
	if err := db.Model(&room).Where("id = ?", client.Match.ID).Update("is_end", true).Error; err != nil {
		log.Printf("Failed to mark chat room as ended: %v", err)
		return
	}

	// Clear the match details in the server for both participants
	firstUserID := client.Match.FirstUserID.String()
	secondUserID := client.Match.SecondUserID.String()

	// Notify both clients that the match has ended
	sendFreeResponse(firstUserID)
	sendFreeResponse(secondUserID)

	// Clear match info from clients map
	clientsM.Lock()
	if firstClient, ok := clients[firstUserID]; ok {
		firstClient.Match = nil
		firstClient.Queue = nil
	}
	if secondClient, ok := clients[secondUserID]; ok {
		secondClient.Match = nil
		secondClient.Queue = nil
	}
	clientsM.Unlock()
}

func sendFreeResponse(userID string) {
	if client, ok := clients[userID]; ok && client.Ws != nil {
		client.Ws.WriteMessage(websocket.TextMessage, []byte(`{"event": "end", "data": ""}`))
	}
}

func findOrCreateTherapyRoom(userID, therapistID string) (*models.ChatRoom, error) {
	var room models.ChatRoom
	// Try to find an existing room where is_end is false and type is 'consultation'
	if err := db.Where("((first_user_id = ? AND second_user_id = ?) OR (first_user_id = ? AND second_user_id = ?)) AND is_end = false AND type = 'consultation'",
		userID, therapistID, therapistID, userID).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new room if not found
			newRoom := models.ChatRoom{
				FirstUserID:  uuid.MustParse(userID),
				SecondUserID: uuid.MustParse(therapistID),
				Type:         "consultation",
				IsEnd:        false,
			}
			if err := db.Create(&newRoom).Error; err != nil {
				return nil, err
			}
			return &newRoom, nil
		}
		return nil, err
	}
	return &room, nil
}

func sendEnterRoomEvent(userID string, roomID uuid.UUID) {
	client, exists := clients[userID]
	if !exists {
		return
	}
	eventData := fmt.Sprintf(`{"event": "enter_room", "data": "%s"}`, roomID)
	client.Ws.WriteMessage(websocket.TextMessage, []byte(eventData))
}

func findChatRoomByID(roomID string) (*models.ChatRoom, bool) {
	var room models.ChatRoom
	if err := db.Where("id = ?", roomID).First(&room).Error; err != nil {
		log.Printf("Error finding room by ID: %v", err)
		return nil, false
	}
	return &room, true
}

func sendMessageToRoom(userID, roomID string, messageContent string) {
	room, exists := findChatRoomByID(roomID)
	if !exists {
		log.Printf("No room found with ID: %s", roomID)
		return
	}

	if room.FirstUserID.String() != userID && room.SecondUserID.String() != userID {
		log.Printf("User %s not part of the room %s", userID, roomID)
		return
	}

	if ok, reason := canSendChatMessage(userID); !ok {
		notifyLimitReached(userID, reason)
		return
	}

	// Create the chat message
	chatMessage := models.ChatMessage{
		SenderID:       uuid.MustParse(userID),
		ChatRoomID:     uuid.MustParse(roomID),
		MessageContent: messageContent,
		SentAt:         time.Now(),
	}

	// Save the chat message to the database
	if result := db.Create(&chatMessage); result.Error != nil {
		log.Printf("Failed to save chat message: %v", result.Error)
		return
	}

	// Prepare the data to be sent to both participants
	data := map[string]interface{}{
		"event": "messageTherapis",
		"data":  chatMessage,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error marshalling message data: %v", err)
		return
	}

	// Send message to both participants
	sendMessageToClient(room.FirstUserID.String(), jsonData)
	sendMessageToClient(room.SecondUserID.String(), jsonData)
}

func sendMessageToClient(userID string, message []byte) {
	clientsM.Lock()
	client, ok := clients[userID]
	clientsM.Unlock()

	if ok && client.Ws != nil {
		if err := client.Ws.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("Error sending message to user %s: %v", userID, err)
		}
	}
}

func canSendChatMessage(userID string) (bool, string) {
	if subscriptionService == nil {
		return true, ""
	}

	subscription, err := subscriptionService.GetSubscriptionForUser(userID)
	if err != nil {
		log.Printf("Subscription lookup failed for user %s: %v", userID, err)
		return false, "subscription check failed"
	}

	// Premium with active status is unlimited.
	if subscription.Plan == models.SubscriptionPlanPremium && subscription.Status == models.SubscriptionStatusActive {
		return true, ""
	}

	// Free or inactive premium falls back to daily limit.
	if subscription.ChatLimitPerDay == nil {
		return true, ""
	}

	userUUID, err := uuid.Parse(userID)
	if err != nil {
		log.Printf("Invalid user id for chat limit: %v", err)
		return false, "invalid user id"
	}

	startOfDay := time.Now().Truncate(24 * time.Hour)
	var sentCount int64
	if err := db.Model(&models.ChatMessage{}).
		Where("sender_id = ? AND sent_at >= ?", userUUID, startOfDay).
		Count(&sentCount).Error; err != nil {
		log.Printf("Failed to count messages for limit: %v", err)
		return false, "could not verify chat limit"
	}

	if sentCount >= int64(*subscription.ChatLimitPerDay) {
		return false, fmt.Sprintf("daily chat limit reached (%d messages)", *subscription.ChatLimitPerDay)
	}

	return true, ""
}

func notifyLimitReached(userID, reason string) {
	msg := map[string]string{
		"event": "limit_reached",
		"data":  reason,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal limit message: %v", err)
		return
	}

	clientsM.Lock()
	client := clients[userID]
	clientsM.Unlock()
	if client != nil && client.Ws != nil {
		_ = client.Ws.WriteMessage(websocket.TextMessage, payload)
	}
}
