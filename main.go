package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// CommandRequest is the JSON payload sent by Dooray when a slash command is invoked.
type CommandRequest struct {
	TenantID     string `json:"tenantId"`
	TenantDomain string `json:"tenantDomain"`
	ChannelID    string `json:"channelId"`
	ChannelName  string `json:"channelName"`
	UserID       string `json:"userId"`
	Command      string `json:"command"`
	Text         string `json:"text"`
	ResponseURL  string `json:"responseUrl"`
	AppToken     string `json:"appToken"`
	CmdToken     string `json:"cmdToken"`
	TriggerID    string `json:"triggerId"`
}

// ActionCallback is the JSON payload sent when a user clicks a button action.
type ActionCallback struct {
	Tenant struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	} `json:"tenant"`
	Channel struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"channel"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	ResponseURL string            `json:"responseUrl"`
	CmdToken    string            `json:"cmdToken"`
	TriggerID   string            `json:"triggerId"`
	CallbackID  string            `json:"callbackId"`
	ActionName  string            `json:"actionName"`
	ActionValue string            `json:"actionValue"`
	Type        string            `json:"type"`
	Submission  map[string]string `json:"submission"`
}

type DialogOpenRequest struct {
	Token      string `json:"token"`
	TriggerID  string `json:"triggerId"`
	CallbackID string `json:"callbackId"`
	Dialog     Dialog `json:"dialog"`
}

type Dialog struct {
	CallbackID  string    `json:"callbackId"`
	Title       string    `json:"title"`
	SubmitLabel string    `json:"submitLabel"`
	Elements    []Element `json:"elements"`
}

type Element struct {
	Type        string   `json:"type"`
	Subtype     string   `json:"subtype,omitempty"`
	Label       string   `json:"label"`
	Name        string   `json:"name"`
	Value       string   `json:"value,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Hint        string   `json:"hint,omitempty"`
	Optional    bool     `json:"optional,omitempty"`
	Options     []Option `json:"options,omitempty"`
	MinLength   int      `json:"minLength,omitempty"`
	MaxLength   int      `json:"maxLength,omitempty"`
}

type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

var zones = []string{"pubo", "fino", "govo", "govi", "pppng"}

const (
	zonesCallbackPrefix  = "amb-zones:"
	actionCallbackPrefix = "amb-action:"
	submitCallbackPrefix = "amb-submit:"
)

var appToken string

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	appToken = os.Getenv("DOORAY_APP_TOKEN")
	if appToken == "" {
		log.Println("WARNING: DOORAY_APP_TOKEN is not set, token validation disabled")
	}

	http.HandleFunc("/command", handleCommand)
	http.HandleFunc("POST /interactive", handleInteractive)
	http.HandleFunc("/health", handleHealth)

	log.Printf("Server starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleCommand(w http.ResponseWriter, r *http.Request) {

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req CommandRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if appToken != "" && req.AppToken != appToken {
		log.Printf("Invalid appToken: %s", req.AppToken)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	msg := buildZoneMessage(nil)
	msg["responseType"] = "ephemeral"
	msg["text"] = "AMB 공유 - Zone을 선택하세요"

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(msg)
}

func openDialog(tenantDomain, channelID, cmdToken, triggerID string, selectedZones []string) {
	cbID := submitCallbackPrefix + strings.Join(selectedZones, ",")

	dialogReq := DialogOpenRequest{
		Token:      cmdToken,
		TriggerID:  triggerID,
		CallbackID: cbID,
		Dialog: Dialog{
			CallbackID:  cbID,
			Title:       "AMB 공유",
			SubmitLabel: "공유",
			Elements: []Element{
				{
					Type:        "text",
					Subtype:     "url",
					Label:       "업무 URL",
					Name:        "task_url",
					Placeholder: "업무 URL을 입력하세요",
				},
			},
		},
	}

	payload, err := json.Marshal(dialogReq)
	if err != nil {
		log.Printf("Failed to marshal dialog request: %v", err)
		return
	}

	url := fmt.Sprintf("https://%s/messenger/api/channels/%s/dialogs", tenantDomain, channelID)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("Failed to create dialog request: %v", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("token", cmdToken)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Printf("Failed to open dialog: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("Dialog open failed [%d]: %s", resp.StatusCode, string(respBody))
	}
}

func handleInteractive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var cb ActionCallback
	if err := json.Unmarshal(body, &cb); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Interactive: type=%s callbackId=%s actionName=%s actionValue=%s", cb.Type, cb.CallbackID, cb.ActionName, cb.ActionValue)

	// Dialog submission
	if cb.Type == "dialog_submission" && strings.HasPrefix(cb.CallbackID, submitCallbackPrefix) {
		handleDialogSubmission(w, cb)
		return
	}

	// Button actions on zone selection message
	if strings.HasPrefix(cb.CallbackID, zonesCallbackPrefix) || strings.HasPrefix(cb.CallbackID, actionCallbackPrefix) {
		handleButtonAction(w, cb)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func parseSelectedZones(callbackID string) []string {
	// Extract zones from callbackId like "amb-zones:pubo,fino" or "amb-action:pubo,fino"
	var raw string
	if strings.HasPrefix(callbackID, zonesCallbackPrefix) {
		raw = strings.TrimPrefix(callbackID, zonesCallbackPrefix)
	} else if strings.HasPrefix(callbackID, actionCallbackPrefix) {
		raw = strings.TrimPrefix(callbackID, actionCallbackPrefix)
	} else if strings.HasPrefix(callbackID, submitCallbackPrefix) {
		raw = strings.TrimPrefix(callbackID, submitCallbackPrefix)
	}
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func toggleZone(selected []string, zone string) []string {
	for i, z := range selected {
		if z == zone {
			return append(selected[:i], selected[i+1:]...)
		}
	}
	return append(selected, zone)
}

func handleButtonAction(w http.ResponseWriter, cb ActionCallback) {
	// State is encoded in actionValue, not callbackId
	// toggle: actionValue = resulting zones after toggle (comma-separated)
	// next: actionValue = current selected zones (comma-separated)
	// cancel: actionValue = "cancel"

	switch cb.ActionName {
	case "toggle":
		var selected []string
		if cb.ActionValue != "" {
			selected = strings.Split(cb.ActionValue, ",")
		}
		msg := buildZoneMessage(selected)
		msg["deleteOriginal"] = true
		msg["responseType"] = "ephemeral"
		msg["channelId"] = cb.Channel.ID
		msg["text"] = "AMB 공유 - Zone을 선택하세요"
		go postToResponseURL(cb.ResponseURL, msg)
		w.WriteHeader(http.StatusOK)

	case "next":
		if cb.ActionValue == "" {
			msg := buildZoneMessage(nil)
			msg["deleteOriginal"] = true
			msg["responseType"] = "ephemeral"
			msg["channelId"] = cb.Channel.ID
			msg["text"] = "⚠️ 최소 하나의 Zone을 선택해주세요"
			go postToResponseURL(cb.ResponseURL, msg)
			w.WriteHeader(http.StatusOK)
			return
		}
		selected := strings.Split(cb.ActionValue, ",")
		// Delete zone selection message, then open dialog
		go func() {
			postToResponseURL(cb.ResponseURL, map[string]interface{}{
				"deleteOriginal": true,
				"channelId":      cb.Channel.ID,
			})
			openDialog(cb.Tenant.Domain, cb.Channel.ID, cb.CmdToken, cb.TriggerID, selected)
		}()
		w.WriteHeader(http.StatusOK)

	case "cancel":
		go postToResponseURL(cb.ResponseURL, map[string]interface{}{
			"deleteOriginal": true,
			"channelId":      cb.Channel.ID,
		})
		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusOK)
	}
}

func postToResponseURL(responseURL string, msg map[string]interface{}) {
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal responseUrl payload: %v", err)
		return
	}

	log.Printf("POST to responseUrl: %s", string(payload))

	resp, err := http.Post(responseURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("Failed to POST to responseUrl: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("responseUrl POST failed [%d]: %s", resp.StatusCode, string(respBody))
	}
}

func handleDialogSubmission(w http.ResponseWriter, cb ActionCallback) {
	selectedZones := parseSelectedZones(cb.CallbackID)
	taskURL := cb.Submission["task_url"]

	if len(selectedZones) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{
				{"name": "task_url", "error": "Zone 선택이 유실되었습니다. 다시 시도해주세요."},
			},
		})
		return
	}

	if taskURL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{
				{"name": "task_url", "error": "업무 URL을 입력해주세요"},
			},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))

	go sendMessage(cb.ResponseURL, cb.Channel.ID, selectedZones, taskURL)
}

func buildZoneMessage(selectedZones []string) map[string]interface{} {
	selectedSet := make(map[string]bool)
	for _, z := range selectedZones {
		selectedSet[z] = true
	}

	zoneButtons := make([]map[string]interface{}, 0, len(zones))
	for _, zone := range zones {
		// Compute resulting state after toggling this zone
		toggled := toggleZone(append([]string{}, selectedZones...), zone)
		btn := map[string]interface{}{
			"type":  "button",
			"name":  "toggle",
			"value": strings.Join(toggled, ","),
		}
		if selectedSet[zone] {
			btn["text"] = "✓ " + zone
			btn["style"] = "primary"
		} else {
			btn["text"] = zone
		}
		zoneButtons = append(zoneButtons, btn)
	}

	zonesStr := strings.Join(selectedZones, ",")

	var statusText string
	if len(selectedZones) == 0 {
		statusText = "선택된 Zone: 없음"
	} else {
		statusText = "선택된 Zone: " + strings.Join(selectedZones, ", ")
	}

	return map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"callbackId": zonesCallbackPrefix,
				"actions":    zoneButtons,
			},
			{
				"callbackId": actionCallbackPrefix,
				"text":       statusText,
				"actions": []map[string]interface{}{
					{
						"type":  "button",
						"name":  "next",
						"text":  "다음",
						"value": zonesStr,
						"style": "primary",
					},
					{
						"type":  "button",
						"name":  "cancel",
						"text":  "취소",
						"value": "cancel",
						"style": "danger",
					},
				},
			},
		},
	}
}

func sendMessage(responseURL, channelID string, selectedZones []string, taskURL string) {
	zonesText := strings.Join(selectedZones, ", ")

	msg := map[string]interface{}{
		"channelId":      channelID,
		"responseType":   "inChannel",
		"deleteOriginal": true,
		"text":           "AMB 공유",
		"attachments": []map[string]interface{}{
			{
				"title":     "AMB 배포 공유",
				"titleLink": taskURL,
				"color":     "#4757C4",
				"fields": []map[string]interface{}{
					{
						"title": "Zone",
						"value": zonesText,
						"short": true,
					},
					{
						"title": "업무 URL",
						"value": taskURL,
						"short": false,
					},
				},
			},
		},
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}

	resp, err := http.Post(responseURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("Failed to send message: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("sendMessage failed [%d]: %s", resp.StatusCode, string(respBody))
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
