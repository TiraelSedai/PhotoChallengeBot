package admin

import (
	"context"
	"reflect"
	"testing"
)

func TestAdminRouterRoutesDeletePhotoBeforeCreateChallenge(t *testing.T) {
	deletePhoto := &MoqDeletePhotoCommandHandler{
		HandlesFunc: func(text string) bool {
			_, handled, _ := parseDeletePhotoCommand(text, "PhotoChallengeBot")
			return handled
		},
	}
	finishVote := &MoqFinishVoteCommandHandler{}
	createChallenge := &MoqAdminMessageHandler{}
	router := NewRouter(RouterConfig{DeletePhoto: deletePhoto, FinishVote: finishVote, CreateChallenge: createChallenge})

	if err := router.HandleAdminChatMessage(context.Background(), adminMessage("/delete_photo @author")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	deleteCalls := deletePhoto.HandleAdminChatMessageCalls()
	if len(deleteCalls) != 1 || !reflect.DeepEqual([]string{deleteCalls[0].Message.Text}, []string{"/delete_photo @author"}) {
		t.Fatalf("delete calls = %#v", deleteCalls)
	}
	if len(createChallenge.HandleAdminChatMessageCalls()) != 0 {
		t.Fatalf("create calls = %#v, want none", createChallenge.HandleAdminChatMessageCalls())
	}
}

func TestNewRouterPanicsOnNilFinishVoteHandler(t *testing.T) {
	deletePhoto := &MoqDeletePhotoCommandHandler{}
	createChallenge := &MoqAdminMessageHandler{}
	defer func() {
		if recover() == nil {
			t.Fatal("NewRouter() did not panic")
		}
	}()
	NewRouter(RouterConfig{DeletePhoto: deletePhoto, CreateChallenge: createChallenge})
}

func TestAdminRouterRoutesOtherMessagesToCreateChallenge(t *testing.T) {
	deletePhoto := &MoqDeletePhotoCommandHandler{
		HandlesFunc: func(text string) bool {
			_, handled, _ := parseDeletePhotoCommand(text, "PhotoChallengeBot")
			return handled
		},
	}
	finishVote := &MoqFinishVoteCommandHandler{}
	createChallenge := &MoqAdminMessageHandler{}
	router := NewRouter(RouterConfig{DeletePhoto: deletePhoto, FinishVote: finishVote, CreateChallenge: createChallenge})

	if err := router.HandleAdminChatMessage(context.Background(), adminMessage("/challenge")); err != nil {
		t.Fatalf("HandleAdminChatMessage() error = %v", err)
	}

	if len(deletePhoto.HandleAdminChatMessageCalls()) != 0 {
		t.Fatalf("delete calls = %#v, want none", deletePhoto.HandleAdminChatMessageCalls())
	}
	createCalls := createChallenge.HandleAdminChatMessageCalls()
	if len(createCalls) != 1 || !reflect.DeepEqual([]string{createCalls[0].Message.Text}, []string{"/challenge"}) {
		t.Fatalf("create calls = %#v", createCalls)
	}
}
