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

// DialogSubmission is the JSON payload sent when a user submits a dialog.
type DialogSubmission struct {
	Type    string `json:"type"`
	Tenant  struct {
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
	ResponseURL    string            `json:"responseUrl"`
	CmdToken       string            `json:"cmdToken"`
	UpdateCmdToken string            `json:"updateCmdToken"`
	CallbackID     string            `json:"callbackId"`
	Submission     map[string]string `json:"submission"`
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

const callbackID = "amb-share"

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
	http.HandleFunc("/interactive", handleInteractive)
	http.HandleFunc("/health", handleHealth)

	log.Printf("Server starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleCommand(w http.ResponseWriter, r *http.Request) {
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

	log.Printf("Command request: %s", string(body))

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

	go openDialog(req)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{})
}

func openDialog(req CommandRequest) {
	elements := make([]Element, 0, len(zones)+1)

	for _, zone := range zones {
		elements = append(elements, Element{
			Type:  "select",
			Label: zone,
			Name:  "zone_" + zone,
			Value: "false",
			Options: []Option{
				{Label: "O", Value: "true"},
				{Label: "X", Value: "false"},
			},
		})
	}

	elements = append(elements, Element{
		Type:        "text",
		Subtype:     "url",
		Label:       "업무 URL",
		Name:        "task_url",
		Placeholder: "업무 URL을 입력하세요",
	})

	dialogReq := DialogOpenRequest{
		Token:      req.CmdToken,
		TriggerID:  req.TriggerID,
		CallbackID: callbackID,
		Dialog: Dialog{
			CallbackID:  callbackID,
			Title:       "AMB 공유",
			SubmitLabel: "공유",
			Elements:    elements,
		},
	}

	payload, err := json.Marshal(dialogReq)
	if err != nil {
		log.Printf("Failed to marshal dialog request: %v", err)
		return
	}

	url := fmt.Sprintf("https://%s/messenger/api/channels/%s/dialogs", req.TenantDomain, req.ChannelID)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("Failed to create dialog request: %v", err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("token", req.CmdToken)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Printf("Failed to open dialog: %v", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("Dialog open response [%d]: %s", resp.StatusCode, string(respBody))
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

	log.Printf("Interactive request: %s", string(body))

	var sub DialogSubmission
	if err := json.Unmarshal(body, &sub); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if sub.Type != "dialog_submission" || sub.CallbackID != callbackID {
		w.WriteHeader(http.StatusOK)
		return
	}

	selectedZones := make([]string, 0)
	for _, zone := range zones {
		if sub.Submission["zone_"+zone] == "true" {
			selectedZones = append(selectedZones, zone)
		}
	}

	taskURL := sub.Submission["task_url"]

	if len(selectedZones) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{
				{"name": "zone_pubo", "error": "최소 하나의 zone을 선택해주세요"},
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

	zonesText := strings.Join(selectedZones, ", ")

	resp := map[string]interface{}{
		"responseType": "inChannel",
		"text":         "AMB 공유",
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

	respJSON, _ := json.Marshal(resp)
	log.Printf("Interactive response: %s", string(respJSON))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respJSON)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
