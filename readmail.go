package main

import (
	"bytes"
	"code.google.com/p/go-imap/go1/imap"
	"errors"
	"fmt"
	ui "github.com/gizak/termui"
	"io"
	"io/ioutil"
	"log"
	"math"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"strings"
	"time"
)

var USERNAME = os.Getenv("GOMAIL_USER")
var PASSWORD = os.Getenv("GOMAIL_PASS")
var IMAP_SERVER = os.Getenv("GOMAIL_IMAP_SERVER")

// This usually seems to retrieve multi-part messages in MIME format,
// which is what we want, so we can select the text/plain component.
var BODY_PART_NAME = "RFC822"

var _file, _ = os.OpenFile("gomail.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
var LOG = log.New(_file, "gomail: ", log.Ldate|log.Lshortfile|log.Ltime)

var (
	c   *imap.Client
	cmd *imap.Command
	rsp *imap.Response
)

func panicMaybe(err error) {
	if err != nil {
		panic(err)
	}
}

func mustSucceed(result interface{}, err error) interface{} {
	panicMaybe(err)
	return result
}

// Get a Message out of a MessageInfo attribute.
func messageAttr(message *imap.MessageInfo, attrName string) *mail.Message {
	content := imap.AsBytes(message.Attrs[attrName])
	msg, _ := mail.ReadMessage(bytes.NewReader(content))
	//	panicMaybe(err)
	return msg
}

// Get the reader for a particular Part of a multipart message.
// This function is called recursively until we find a text/plain part.
func partReader(part *multipart.Part) (io.Reader, error) {
	mediaType, params, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		LOG.Println("inner-part boundary: %s\n", params["boundary"])
		mpReader := multipart.NewReader(part, params["boundary"])
		for {
			nextPart, err := mpReader.NextPart()
			if err == io.EOF {
				LOG.Println("Reached EOF while scanning inner message parts.")
				return nil, err
			}
			panicMaybe(err)
			nextReader, err := partReader(nextPart)
			if nextReader != nil {
				return nextReader, err
			}
		}
	} else if mediaType == "text/plain" {
		return part, nil
	}
	return nil, errors.New("Not a text/plain part")
}

func messageReader(message *imap.MessageInfo) (io.Reader, error) {
	// Get content-type of the message.
	msg := messageAttr(message, BODY_PART_NAME)
	if msg != nil {
		mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(mediaType, "multipart/") {
			mpReader := multipart.NewReader(msg.Body, params["boundary"])
			for {
				part, err := mpReader.NextPart()
				textPlainPart, err := partReader(part)
				if err == io.EOF {
					LOG.Println("Reached EOF while reading multipart message")
					return nil, err
				}
				panicMaybe(err)
				if textPlainPart != nil {
					return textPlainPart, err
				}
			}
		} else if mediaType == "text/plain" {
			return msg.Body, nil
		}
		return nil, errors.New("No text/plain message part found")
	}
	return nil, errors.New("Could not find message body")
}

func readMessage(message *imap.MessageInfo) {
	set := new(imap.SeqSet)
	set.AddNum(message.Seq)
	cmd, err := imap.Wait(c.Fetch(set, BODY_PART_NAME))
	panicMaybe(err)

	reader, err := messageReader(cmd.Data[0].MessageInfo())
	panicMaybe(err)
	messageBody, err := ioutil.ReadAll(reader)
	messageBodyStr := string(messageBody)

	if len(messageBodyStr) <= 0 {
		LOG.Printf("Message body was empty or could not be retrieved: +%v\n", err)
		return
	}
	msgBox := ui.NewPar(messageBodyStr)
	msgBox.Border.Label = "demo list"
	msgBox.Height = ui.TermHeight()
	msgBox.Width = ui.TermWidth()
	msgBox.Y = 0

	ui.Render(msgBox)
}

func listMessages(messages []*imap.MessageInfo) {
	selectedIndex := 0

	messageStrings := func(index int) []string {
		strs := make([]string, len(messages), len(messages))
		for idx, message := range messages {
			header := imap.AsBytes(message.Attrs["RFC822.HEADER"])
			if msg, _ := mail.ReadMessage(bytes.NewReader(header)); msg != nil {
				subject := msg.Header.Get("Subject")
				if idx == index {
					strs = append(strs, "> "+subject)
				} else {
					strs = append(strs, "  "+subject)
				}
			}
		}
		return strs
	}

	err := ui.Init()
	if err != nil {
		panic(err)
	}
	defer ui.Close()

	ui.UseTheme("helloworld")

	lst := ui.NewList()
	lst.Items = messageStrings(selectedIndex)
	lst.ItemFgColor = ui.ColorGreen
	lst.Border.Label = "demo list"
	lst.Height = ui.TermHeight()
	lst.Width = ui.TermWidth()
	lst.Y = 0

	ui.Render(lst)

	redraw := make(chan bool)

	for {
		select {
		case e := <-ui.EventCh():
			if e.Type == ui.EventKey {
				switch e.Key {
				case ui.KeyEsc:
					return
				case ui.KeyArrowUp:
					selectedIndex = int(math.Max(
						0,
						float64(selectedIndex-1)))
					go func() { redraw <- true }()
				case ui.KeyArrowDown:
					selectedIndex = int(math.Min(
						float64(len(messages)-1),
						float64(selectedIndex+1)))
					go func() { redraw <- true }()
				case ui.KeyEnter:
					readMessage(messages[selectedIndex])
				}
			}
		case <-redraw:
			lst.Items = messageStrings(selectedIndex)
			ui.Render(lst)
		}
	}

}

func main() {
	// Source: https://godoc.org/code.google.com/p/go-imap/go1/imap#example-Client
	// Note: most of error handling code is omitted for brevity
	//

	// Connect to the server
	c, _ = imap.DialTLS(IMAP_SERVER, nil)

	// Remember to log out and close the connection when finished
	defer c.Logout(30 * time.Second)

	// Print server greeting (first response in the unilateral server data queue)
	fmt.Println("Server says hello:", c.Data[0].Info)
	c.Data = nil

	// Enable encryption, if supported by the server
	if c.Caps["STARTTLS"] {
		c.StartTLS(nil)
	}

	// Authenticate
	if c.State() == imap.Login {
		c.Login(USERNAME, PASSWORD)
	}

	// List all top-level mailboxes, wait for the command to finish
	cmd, _ = imap.Wait(c.List("", "%"))

	// Print mailbox information
	fmt.Println("\nTop-level mailboxes:")
	for _, rsp = range cmd.Data {
		fmt.Println("|--", rsp.MailboxInfo())
	}

	// Check for new unilateral server data responses
	for _, rsp = range c.Data {
		fmt.Println("Server data:", rsp)
	}
	c.Data = nil

	// Open a mailbox (synchronous command - no need for imap.Wait)
	c.Select("INBOX", true)
	fmt.Print("\nMailbox status:\n", c.Mailbox)

	// Fetch the headers of the 10 most recent messages
	set, _ := imap.NewSeqSet("")
	if c.Mailbox.Messages >= 10 {
		set.AddRange(c.Mailbox.Messages-19, c.Mailbox.Messages)
	} else {
		set.Add("1:*")
	}
	cmd, _ = imap.Wait(c.Fetch(set, "RFC822.HEADER"))

	messages := make([]*imap.MessageInfo, 0, 0)
	for _, rsp = range cmd.Data {
		// May not be necessary to check for nil here.
		if msg := rsp.MessageInfo(); msg != nil {
			messages = append(messages, msg)
		}
	}
	listMessages(messages)

	// // Process responses while the command is running
	// fmt.Println("\nMost recent messages:")
	// for cmd.InProgress() {
	// 	// Wait for the next response (no timeout)
	// 	c.Recv(-1)

	// 	// Process command data
	// 	for _, rsp = range cmd.Data {
	// 		header := imap.AsBytes(rsp.MessageInfo().Attrs["RFC822.HEADER"])
	// 		if msg, _ := mail.ReadMessage(bytes.NewReader(header)); msg != nil {
	// 			fmt.Println("|--", msg.Header.Get("Subject"))
	// 		}
	// 	}
	// 	cmd.Data = nil

	// 	// Process unilateral server data
	// 	for _, rsp = range c.Data {
	// 		fmt.Println("Server data:", rsp)
	// 	}
	// 	c.Data = nil
	// }

	// // Check command completion status
	// if rsp, err := cmd.Result(imap.OK); err != nil {
	// 	if err == imap.ErrAborted {
	// 		fmt.Println("Fetch command aborted")
	// 	} else {
	// 		fmt.Println("Fetch error:", rsp.Info)
	// 	}
	// }

	//	render()
}
