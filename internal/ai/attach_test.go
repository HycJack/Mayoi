package ai

import (
	"testing"

	"github.com/HycJack/pi-ai-go/core"
)

// TestAQuestionWithPicturesCarriesThem: the model is given the text and the
// pictures after it, as the blocks both protocols take.
func TestAQuestionWithPicturesCarriesThem(t *testing.T) {
	msg := userMessage(Run{
		Prompt: "what is this?",
		Pictures: []Picture{
			{Name: "a.png", MIMEType: "image/png", Data: []byte("png")},
			{Name: "b.jpg", MIMEType: "image/jpeg", Data: []byte("jpg")},
		},
	})
	user, ok := msg.(core.UserMessage)
	if !ok {
		t.Fatalf("the question is a %T", msg)
	}
	blocks, ok := user.Content.([]core.ContentBlock)
	if !ok {
		t.Fatalf("the content is a %T", user.Content)
	}
	if len(blocks) != 3 {
		t.Fatalf("the question has %d blocks", len(blocks))
	}
	if text, ok := blocks[0].(core.TextContent); !ok || text.Text != "what is this?" {
		t.Errorf("the first block is %#v", blocks[0])
	}
	for i, want := range []struct{ mime, data string }{{"image/png", "cG5n"}, {"image/jpeg", "anBn"}} {
		img, ok := blocks[i+1].(core.ImageContent)
		if !ok {
			t.Fatalf("block %d is %#v", i+1, blocks[i+1])
		}
		if img.MimeType != want.mime || img.Data != want.data {
			t.Errorf("block %d is %+v", i+1, img)
		}
	}
}

// TestAQuestionWithoutPicturesIsItsText: a question of words stays a string,
// which every model of every protocol reads.
func TestAQuestionWithoutPicturesIsItsText(t *testing.T) {
	msg := userMessage(Run{Prompt: "just words"})
	user, ok := msg.(core.UserMessage)
	if !ok {
		t.Fatalf("the question is a %T", msg)
	}
	if text, ok := user.Content.(string); !ok || text != "just words" {
		t.Errorf("the content is %#v", user.Content)
	}
}

// TestAChatKeepsTheQuestionAndNotThePictures: a saved chat says a picture
// was there, and does not carry its bytes.
func TestAChatKeepsTheQuestionAndNotThePictures(t *testing.T) {
	msgs := []core.Message{
		userMessage(Run{
			Prompt:   "what is this?",
			Pictures: []Picture{{Name: "a.png", MIMEType: "image/png", Data: []byte("png")}},
		}),
		core.AssistantMessage{Role: "assistant", Content: []core.ContentBlock{core.TextContent{Type: "text", Text: "a cat"}}},
	}
	kept := WithoutPictures(msgs)
	user, ok := kept[0].(core.UserMessage)
	if !ok {
		t.Fatalf("the question is a %T", kept[0])
	}
	blocks, ok := user.Content.([]core.ContentBlock)
	if !ok {
		t.Fatalf("the content is a %T", user.Content)
	}
	if len(blocks) != 2 {
		t.Fatalf("the question has %d blocks", len(blocks))
	}
	if text, ok := blocks[0].(core.TextContent); !ok || text.Text != "what is this?" {
		t.Errorf("the words are %#v", blocks[0])
	}
	if note, ok := blocks[1].(core.TextContent); !ok || note.Text != "[a picture]" {
		t.Errorf("the picture is kept as %#v", blocks[1])
	}
	// What was not a question of pictures is kept as it was.
	if _, err := kept[1].(core.AssistantMessage); !err {
		t.Error("the answer did not survive the trip")
	}
}
