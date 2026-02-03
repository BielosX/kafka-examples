package internal

import (
	"context"
	"errors"
	"fmt"
	"protobuf/gen"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
)

const (
	ConnectionInputState = iota
	ChatState
)

type ChatModel struct {
	connection   *websocket.Conn
	focusIndex   int
	inputs       []textinput.Model
	viewport     viewport.Model
	chatInput    textarea.Model
	cursorMode   cursor.Mode
	state        int
	err          error
	msgChan      chan *messages.ServerResponse
	errChan      chan error
	readerCancel context.CancelFunc
	messages     []string
}

var (
	focusedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	noStyle        = lipgloss.NewStyle()
	blurredStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	focusedButton  = focusedStyle.Render("[ Connect ]")
	blurredButton  = fmt.Sprintf("[ %s ]", blurredStyle.Render("Connect"))
	importantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f5bf42"))
	criticalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#731414"))
)

func NewChatModel() *ChatModel {
	chatInput := textarea.New()
	chatInput.SetWidth(30)
	chatInput.SetHeight(3)
	chatInput.KeyMap.InsertNewline.SetEnabled(false)
	chatInput.ShowLineNumbers = false
	chatInput.Focus()
	model := &ChatModel{
		state:     ConnectionInputState,
		inputs:    make([]textinput.Model, 3),
		viewport:  viewport.New(80, 5),
		chatInput: chatInput,
		msgChan:   make(chan *messages.ServerResponse, 1024),
		errChan:   make(chan error),
	}
	for i := range model.inputs {
		t := textinput.New()
		t.PromptStyle = noStyle
		t.TextStyle = noStyle
		t.Cursor.Style = noStyle
		t.Width = 256
		t.CharLimit = 32
		switch i {
		case 0:
			t.Placeholder = "Address"
			t.CharLimit = 256
			t.TextStyle = noStyle
			t.PromptStyle = focusedStyle
			t.Cursor.Style = focusedStyle
			t.Focus()
		case 1:
			t.Placeholder = "Room"
		case 2:
			t.Placeholder = "Nick"
		}
		model.inputs[i] = t
	}
	return model
}

func (m *ChatModel) Init() tea.Cmd {
	return textinput.Blink
}

func mod(a, b int) int {
	return ((a % b) + b) % b
}

type ConnectionResult struct {
	Conn  *websocket.Conn
	Error error
}

func (m *ChatModel) connect() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	address := m.inputs[0].Value()
	room := m.inputs[1].Value()
	nick := m.inputs[2].Value()
	url := fmt.Sprintf("%s?nick=%s&room=%s", address, nick, room)
	c, _, err := websocket.Dial(ctx, url, nil)
	return ConnectionResult{
		Conn:  c,
		Error: err,
	}
}

func (m *ChatModel) readMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			t, msg, err := m.connection.Read(ctx)
			if err != nil {
				m.errChan <- err
				continue
			}
			if t != websocket.MessageBinary {
				m.errChan <- errors.New(fmt.Sprintf("received unsupported message type: %s", t.String()))
				continue
			}
			var response messages.ServerResponse
			err = proto.Unmarshal(msg, &response)
			if err != nil {
				m.errChan <- err
				continue
			}
			m.msgChan <- &response
		}
	}
}

type TickMsg time.Time

func doTick() tea.Cmd {
	return tea.Tick(time.Millisecond*200, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (m *ChatModel) handleTick() (tea.Model, tea.Cmd) {
	select {
	case err := <-m.errChan:
		m.err = err
	default:
		// noop
	}
	for i := 0; i < 50; i++ {
		select {
		case msg := <-m.msgChan:
			timestamp := msg.GetTimestamp().AsTime().Format(time.RFC3339)
			from := msg.GetPayload().GetFrom()
			message := msg.GetPayload().GetContent().GetMessage()
			line := fmt.Sprintf("[%s] %s: %s", timestamp, from, message)
			switch msg.GetPayload().GetContent().GetSeverity() {
			case messages.MessageSeverity_NORMAL:
				m.messages = append(m.messages, line)
			case messages.MessageSeverity_IMPORTANT:
				m.messages = append(m.messages, importantStyle.Render(line))
			case messages.MessageSeverity_CRITICAL:
				m.messages = append(m.messages, criticalStyle.Render(line))
			}
		default:
			// noop
		}
	}
	m.viewport.SetContent(lipgloss.NewStyle().Width(m.viewport.Width).Render(strings.Join(m.messages, "\n")))
	m.viewport.GotoBottom()
	return m, doTick()
}

func (m *ChatModel) updateConnectionInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ConnectionResult:
		if msg.Error != nil {
			m.err = msg.Error
		} else {
			m.state = ChatState
			m.connection = msg.Conn
			ctx, cancel := context.WithCancel(context.Background())
			m.readerCancel = cancel
			go m.readMessages(ctx)
			return m, doTick()
		}
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			if m.focusIndex == 3 {
				return m, m.connect
			}
		case tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyUp, tea.KeyDown:
			if msg.Type == tea.KeyUp {
				m.focusIndex = mod(m.focusIndex-1, 4)
			} else {
				m.focusIndex = (m.focusIndex + 1) % 4
			}
			cmds := make([]tea.Cmd, len(m.inputs))
			for i := range m.inputs {
				if i == m.focusIndex {
					cmds[i] = m.inputs[i].Focus()
					m.inputs[i].PromptStyle = focusedStyle
					m.inputs[i].TextStyle = focusedStyle
					m.inputs[i].Cursor.Style = focusedStyle
					continue
				}
				m.inputs[i].Blur()
				m.inputs[i].PromptStyle = noStyle
				m.inputs[i].TextStyle = noStyle
			}
			return m, tea.Batch(cmds...)
		default:
			break
		}
	}
	cmds := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}
	return m, tea.Batch(cmds...)
}

func (m *ChatModel) writeMessage(msg string, severity *messages.MessageSeverity) error {
	builder := messages.MessageContent_builder{}
	builder.Severity = severity
	builder.Message = proto.String(msg)
	content := builder.Build()
	payload, err := proto.Marshal(content)
	if err != nil {
		return err
	}
	err = m.connection.Write(context.Background(), websocket.MessageBinary, payload)
	if err != nil {
		return err
	}
	return nil
}

func (m *ChatModel) send(severity *messages.MessageSeverity) {
	m.err = m.writeMessage(m.chatInput.Value(), severity)
	m.chatInput.Reset()
}

func (m *ChatModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)
	m.chatInput, tiCmd = m.chatInput.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	switch msg := msg.(type) {
	case TickMsg:
		return m.handleTick()
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			_ = m.connection.Close(websocket.StatusGoingAway, "")
			return m, tea.Quit
		case tea.KeyEnter:
			m.send(messages.MessageSeverity_NORMAL.Enum())
		case tea.KeyCtrlI:
			m.send(messages.MessageSeverity_IMPORTANT.Enum())
		case tea.KeyCtrlC:
			m.send(messages.MessageSeverity_CRITICAL.Enum())
		}
	}
	return m, tea.Batch(tiCmd, vpCmd)
}

func (m *ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case ConnectionInputState:
		return m.updateConnectionInput(msg)
	case ChatState:
		return m.updateChat(msg)
	default:
		return m, nil
	}
}

func (m *ChatModel) View() string {
	var builder strings.Builder

	switch m.state {
	case ConnectionInputState:
		for i := range m.inputs {
			builder.WriteString(m.inputs[i].View())
			builder.WriteRune('\n')
		}
		button := &blurredButton
		if m.focusIndex == 3 {
			button = &focusedButton
		}
		if m.err != nil {
			_, _ = fmt.Fprintf(&builder, "\n%s\n%s\n", *button, errorStyle.Render(m.err.Error()))
		} else {
			_, _ = fmt.Fprintf(&builder, "\n%s\n\n", *button)
		}
	case ChatState:
		if m.err != nil {
			_, _ = fmt.Fprintf(&builder, "%s\n\n%s\n%s\n",
				m.viewport.View(),
				m.chatInput.View(),
				errorStyle.Render(m.err.Error()))
		} else {
			_, _ = fmt.Fprintf(&builder, "%s\n\n%s\n", m.viewport.View(), m.chatInput.View())
		}
	}
	return builder.String()
}
