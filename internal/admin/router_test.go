package admin

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestAdminRouterRoutesDeletePhotoBeforeCreateChallenge(t *testing.T) {
	deletePhoto := &recordingDeletePhotoHandler{recordingAdminHandler: recordingAdminHandler{name: "delete"}}
	createChallenge := &recordingAdminHandler{name: "create"}
	router := NewRouter(RouterConfig{DeletePhoto: deletePhoto, CreateChallenge: createChallenge})

	if err := router.HandleAdminChatMessage(context.Background(), adminMessage("/delete_photo @author")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if !reflect.DeepEqual(deletePhoto.messages, []string{"/delete_photo @author"}) {
		t.Fatalf("delete messages = %v", deletePhoto.messages)
	}
	if len(createChallenge.messages) != 0 {
		t.Fatalf("create messages = %v, want none", createChallenge.messages)
	}
}

func TestAdminRouterRoutesOtherMessagesToCreateChallenge(t *testing.T) {
	deletePhoto := &recordingDeletePhotoHandler{recordingAdminHandler: recordingAdminHandler{name: "delete"}}
	createChallenge := &recordingAdminHandler{name: "create"}
	router := NewRouter(RouterConfig{DeletePhoto: deletePhoto, CreateChallenge: createChallenge})

	if err := router.HandleAdminChatMessage(context.Background(), adminMessage("/challenge")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if len(deletePhoto.messages) != 0 {
		t.Fatalf("delete messages = %v, want none", deletePhoto.messages)
	}
	if !reflect.DeepEqual(createChallenge.messages, []string{"/challenge"}) {
		t.Fatalf("create messages = %v", createChallenge.messages)
	}
}

type recordingAdminHandler struct {
	name     string
	messages []string
}

func (h *recordingAdminHandler) HandleAdminChatMessage(_ context.Context, message *models.Message) error {
	h.messages = append(h.messages, message.Text)
	return nil
}

type recordingDeletePhotoHandler struct {
	recordingAdminHandler
}

func (h *recordingDeletePhotoHandler) Handles(text string) bool {
	_, handled, _ := parseDeletePhotoCommand(text, "PhotoChallengeBot")
	return handled
}
