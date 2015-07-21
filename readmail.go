package main

import (
	"bytes"
	"code.google.com/p/go-imap/go1/imap"
	"errors"
	"fmt"
	ui "github.com/gizak/termui"
	"math"
	// "mime/multipart"
	"log"
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

// var BODY_PART_NAME = "RFC822.TEXT"

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

func retrieveMultipartText(message *imap.MessageInfo) []string {
	return []string{"hi"}
}

func retrieveSimpleText(message *imap.MessageInfo) []string {
	return []string{"world"}
}

// Get the multipart boundary for a multipart MIME message as the first return value.
// The error returned as the second value is non-nil if the message is not multipart.
func multipartBoundary(message *imap.MessageInfo) (string, error) {
	var err error = nil
	// Get content-type of the message.
	contentType := ""
	msg := messageAttr(message, "RFC822")
	if msg != nil {
		contentType = msg.Header.Get("content-type")
	}
	// Parameters within content-type are separated by semicolons.
	contentTypeParams := strings.Split(contentType, ";")
	// Is this a multipart MIME message?
	isMultipart := (strings.Index(contentTypeParams[0], "multipart") >= 0)
	// The delimiter between parts in a multipart MIME message.
	boundary := ""
	if isMultipart {
		for _, param := range contentTypeParams {
			trimmed := strings.TrimSpace(param)
			if paramValue := strings.TrimPrefix(trimmed, "boundary="); paramValue != trimmed {
				boundary = paramValue
				break
			}
		}
	} else {
		err = errors.New("message is not multipart")
	}

	return boundary, err
}

// if message is multipart:
//   get text from multipart message
// else:
//   get text from not multipart message

func readMessage(message *imap.MessageInfo) {
	set := new(imap.SeqSet)
	set.AddNum(message.Seq)
	cmd, err := imap.Wait(c.Fetch(set, BODY_PART_NAME))
	panicMaybe(err)

	var messageBody = ""
	for _, rsp = range cmd.Data {
		msg := messageAttr(rsp.MessageInfo(), BODY_PART_NAME)
		if msg != nil {
			buff := make([]byte, 1000)
			mustSucceed(msg.Body.Read(buff))
			boundary, _ := multipartBoundary(rsp.MessageInfo())
			if len(boundary) > 0 {
				messageBody = "<<< multipart: " + boundary + " >>>" + string(buff)
			} else {
				messageBody = string(buff)
			}
		} else {
			fmt.Println("No body?")
		}
	}

	if messageBody == "" {
		fmt.Println("message body was empty")
		return
	}
	msgBox := ui.NewPar(messageBody)
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
