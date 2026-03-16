package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/store"
)

type msgAction struct {
	rowHovered     bool      // hover state of entire message row
	lastPress      time.Time // for double-click reply detection
	editBtn        widget.Clickable
	deleteBtn      widget.Clickable
	replyBtn       widget.Clickable
	pinBtn         widget.Clickable
	hideBtn        widget.Clickable
	copyBtn        widget.Clickable
	thumbUpBtn     widget.Clickable
	thumbDownBtn   widget.Clickable
	reactEmojiBtn  widget.Clickable     // "+" button to open reaction emoji picker
	reactionBtns   [8]widget.Clickable  // clickable for existing reactions
	pollOptBtns    [10]widget.Clickable // clickable for poll options
	attBtns        [8]widget.Clickable
	vidSaveBtns    [8]widget.Clickable // download button for video attachments
	nameBtn        widget.Clickable // click on author name → UserPopup
	avatarBtn      widget.Clickable // click on avatar → UserPopup
	p2pBtn         widget.Clickable // P2P download button
	downloadAllBtn widget.Clickable // Download All for game server files
	linkPreviewBtn widget.Clickable // click on link preview → open URL
	replyCountBtn  widget.Clickable // click on "N replies" → open thread
	replyRefBtn    widget.Clickable // click on reply reference → open thread
	bookmarkBtn    widget.Clickable
	editHistoryBtn widget.Clickable // click on "(edited)" → show edit history
	links          MsgLinks
	mentions       MsgMentions
	textSels       []widget.Selectable
}
type MessageView struct {
	app        *App
	list       widget.List
	editor     widget.Editor
	editorList widget.List
	sendBtn    widget.Clickable
	uploadBtn  widget.Clickable
	actions    []msgAction

	// Reply state
	ReplyToID      string
	ReplyToContent string
	ReplyToAuthor  string
	cancelReply    widget.Clickable

	// Edit state
	EditingID    string
	editOriginal string
	cancelEdit   widget.Clickable

	// Typing
	lastTypingSent time.Time

	// Scroll-to-load
	loadingOlder bool
	noMoreOlder  bool

	// Upload state (multi-file)
	pendingUploads  []*pendingUpload
	uploadMu        sync.Mutex
	autoSend        bool
	uploadChannelID string // channelID captured during pickFiles()
	autoSendPending bool   // signal from goroutine for auto-send
	uploadBarBtn    widget.Clickable

	// Download state
	pendingDownloads []*pendingDownload
	downloadMu       sync.Mutex

	// P2P transfer bar clickables (for error retry/dismiss)
	p2pBarBtns map[string]*widget.Clickable

	// Link file from storage
	linkFileBtn widget.Clickable

	// Scroll to bottom
	scrollBottomBtn widget.Clickable

	// Pinned messages
	pinsBtn         widget.Clickable
	pinBarBtn       widget.Clickable
	pinUnpinBtn     widget.Clickable
	pinEditBtn      widget.Clickable
	pinDeleteBtn    widget.Clickable
	pinReplyBtn     widget.Clickable
	pinCopyBtn      widget.Clickable
	pinThumbUpBtn   widget.Clickable
	pinThumbDownBtn widget.Clickable
	pinAttBtns      [8]widget.Clickable
	pinLinkBtn      widget.Clickable
	pinLinks        MsgLinks
	pinSels         []widget.Selectable
	showPins        bool
	pinExpanded     bool
	pinnedMsgs      []api.Message
	pinSeenID       string      // ID of last seen pinned message
	pinsList        widget.List // init Axis in layoutPinnedMessages
	pinsListLinks   []MsgLinks
	pinsListSels    [][]widget.Selectable
	pinsListActs    []pinListAction

	// Mention autocomplete
	mentionQuery  string
	mentionBtns   []widget.Clickable
	showMentions  bool
	mentionSelIdx int

	// Poll builder
	pollBtn         widget.Clickable
	showPollBuilder bool
	pollBuilder     *PollBuilder

	// Schedule builder
	scheduleBtn         widget.Clickable
	showScheduleBuilder bool
	scheduleBuilder     *ScheduleBuilder

	// Emoji picker
	emojiBtn         widget.Clickable
	showEmojis       bool
	emojiList        widget.List
	emojiClickBtns   []widget.Clickable
	unicodeEmojiBtns []widget.Clickable
	emojiCategoryIdx int // 0=custom, 1..N=unicode categories
	emojiCatBtns     []widget.Clickable

	// Reaction emoji picker
	reactPickerMsgIdx int // which message index has the picker open (-1=none)
	reactPickerBtns   []widget.Clickable

	// Search
	searchBtn      widget.Clickable
	searchCloseBtn widget.Clickable
	searchEditor   widget.Editor
	searchMore     widget.Clickable
	showSearch     bool
	searchResults  []api.Message
	searchActions  []msgAction
	searchLoading  bool
	searchOffset   int
	searchQuery    string // last searched text
	searchList     widget.List

	// Slow mode error
	slowModeErr   string
	slowModeUntil time.Time

	// Formatting toolbar
	fmtBoldBtn   widget.Clickable
	fmtItalicBtn widget.Clickable
	fmtStrikeBtn widget.Clickable
	fmtCodeBtn   widget.Clickable

	// Edit history overlay
	editHistoryMsgID string
	editHistory      []api.MessageEdit
	showEditHistory  bool
	editHistoryList  widget.List
	editHistoryClose widget.Clickable
}

func NewMessageView(a *App) *MessageView {
	v := &MessageView{app: a}
	v.list.Axis = layout.Vertical
	v.list.ScrollToEnd = true
	v.editor.Submit = true
	v.editorList.Axis = layout.Vertical
	v.editorList.ScrollToEnd = true
	v.searchEditor.Submit = true
	v.searchEditor.SingleLine = true
	v.searchList.Axis = layout.Vertical
	v.reactPickerMsgIdx = -1
	v.reactPickerBtns = make([]widget.Clickable, len(reactionEmojis))
	v.p2pBarBtns = make(map[string]*widget.Clickable)
	// Pre-allocate Unicode emoji buttons
	total := 0
	for _, cat := range UnicodeEmojiCategories {
		total += len(cat.Emojis)
	}
	v.unicodeEmojiBtns = make([]widget.Clickable, total)
	v.emojiCatBtns = make([]widget.Clickable, len(UnicodeEmojiCategories)+1) // +1 for custom
	v.editHistoryList.Axis = layout.Vertical
	return v
}
func (v *MessageView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	conn := v.app.Conn()
	if conn == nil {
		return layoutCentered(gtx, v.app.Theme, "Select a server", ColorTextDim)
	}

	v.app.mu.RLock()
	messages := conn.Messages
	channelName := conn.ActiveChannelName
	channelTopic := ""
	for _, ch := range conn.Channels {
		if ch.ID == conn.ActiveChannelID {
			channelTopic = ch.Topic
			break
		}
	}
	userID := conn.UserID
	myPerms := conn.MyPermissions
	myUsername := ""
	usernames := make(map[string]bool, len(conn.Users))
	usernameToID := make(map[string]string, len(conn.Users))
	for _, u := range conn.Users {
		usernames[u.Username] = true
		usernameToID[u.Username] = u.ID
		if u.ID == userID {
			myUsername = u.Username
		}
	}
	v.app.mu.RUnlock()

	// Ensure action buttons
	if len(v.actions) < len(messages) {
		v.actions = make([]msgAction, len(messages)+20)
	}

	// Mention autocomplete: take focus for proper arrow key navigation
	if v.showMentions {
		candidates := v.getMentionCandidates()
		n := len(candidates)

		// Register event handler for our tag (mention navigation)
		areaStack := clip.Rect{Max: image.Pt(1, 1)}.Push(gtx.Ops)
		event.Op(gtx.Ops, &v.mentionSelIdx)
		areaStack.Pop()
		gtx.Execute(key.FocusCmd{Tag: &v.mentionSelIdx})

		// Process keys: arrows, Tab, Enter, Escape, Backspace + text input
		for {
			ev, ok := gtx.Event(
				key.FocusFilter{Target: &v.mentionSelIdx},
				key.Filter{Focus: &v.mentionSelIdx, Name: key.NameDownArrow},
				key.Filter{Focus: &v.mentionSelIdx, Name: key.NameUpArrow},
				key.Filter{Focus: &v.mentionSelIdx, Name: key.NameTab},
				key.Filter{Focus: &v.mentionSelIdx, Name: key.NameReturn},
				key.Filter{Focus: &v.mentionSelIdx, Name: key.NameEnter},
				key.Filter{Focus: &v.mentionSelIdx, Name: key.NameEscape},
				key.Filter{Focus: &v.mentionSelIdx, Name: key.NameDeleteBackward},
				key.Filter{Focus: &v.mentionSelIdx, Name: ""}, // catch-all for other keys
			)
			if !ok {
				break
			}
			switch e := ev.(type) {
			case key.Event:
				if e.State != key.Press {
					continue
				}
				switch e.Name {
				case key.NameDownArrow:
					if n > 0 {
						v.mentionSelIdx = (v.mentionSelIdx + 1) % n
					}
				case key.NameUpArrow:
					if n > 0 {
						v.mentionSelIdx = (v.mentionSelIdx - 1 + n) % n
					}
				case key.NameTab, key.NameReturn, key.NameEnter:
					if n > 0 {
						idx := v.mentionSelIdx
						if idx >= n {
							idx = 0
						}
						v.insertMention(candidates[idx].Username)
					}
				case key.NameEscape:
					v.showMentions = false
				case key.NameDeleteBackward:
					v.editor.Delete(-1)
					v.updateMentionState()
				}
			case key.EditEvent:
				v.editor.Insert(e.Text)
				v.sendTyping()
				v.updateMentionState()
			}
		}

		// Editor Update still runs (from Layout), but without focus it processes nothing
		// — only returns ChangeEvent if we changed text via Insert/Delete
		for {
			ev, ok := v.editor.Update(gtx)
			if !ok {
				break
			}
			// Ignore — already processed in the mention handler above
			_ = ev
		}
	} else {
		// Return focus to editor if the mention tag has it
		if gtx.Focused(&v.mentionSelIdx) {
			gtx.Execute(key.FocusCmd{Tag: &v.editor})
		}

		// Normal editor events
		for {
			ev, ok := v.editor.Update(gtx)
			if !ok {
				break
			}
			switch ev.(type) {
			case widget.SubmitEvent:
				if v.EditingID != "" {
					v.submitEdit()
				} else {
					v.sendMessage()
				}
			case widget.ChangeEvent:
				v.sendTyping()
				v.updateMentionState()
			}
		}

		// Escape: cancel editing/reply
		for {
			ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
			if !ok {
				break
			}
			if e, ok := ev.(key.Event); ok && e.State == key.Press {
				if v.EditingID != "" {
					v.EditingID = ""
					v.editor.SetText("")
				} else if v.ReplyToID != "" {
					v.ReplyToID = ""
					v.ReplyToContent = ""
					v.ReplyToAuthor = ""
				}
			}
		}
	}

	// Ctrl+F: open search
	for {
		ev, ok := gtx.Event(key.Filter{Name: "F", Required: key.ModCtrl})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			v.showSearch = !v.showSearch
			if v.showSearch {
				gtx.Execute(key.FocusCmd{Tag: &v.searchEditor})
			}
		}
	}

	// Ctrl+V: clipboard paste upload (image)
	for {
		ev, ok := gtx.Event(key.Filter{Name: "V", Required: key.ModCtrl})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			// Try pasting an image from clipboard — only if editor is empty or has focus
			go v.pasteClipboardImage()
		}
	}

	// Search button toggle
	if v.searchBtn.Clicked(gtx) {
		v.showSearch = !v.showSearch
		if v.showSearch {
			gtx.Execute(key.FocusCmd{Tag: &v.searchEditor})
		} else {
			v.searchResults = nil
			v.searchActions = nil
			v.searchQuery = ""
			v.searchOffset = 0
			v.searchEditor.SetText("")
		}
	}

	// Search close button
	if v.searchCloseBtn.Clicked(gtx) {
		v.showSearch = false
		v.searchResults = nil
		v.searchActions = nil
		v.searchQuery = ""
		v.searchOffset = 0
		v.searchEditor.SetText("")
	}

	// Search editor submit (Enter)
	for {
		ev, ok := v.searchEditor.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			q := strings.TrimSpace(v.searchEditor.Text())
			if q != "" {
				v.searchQuery = q
				v.searchOffset = 0
				v.searchLoading = true
				v.searchResults = nil
				v.searchActions = nil
				go v.doSearch(q, 0, false)
			}
		}
	}

	// Search "Load more" button
	if v.searchMore.Clicked(gtx) && v.searchQuery != "" && !v.searchLoading {
		v.searchLoading = true
		go v.doSearch(v.searchQuery, v.searchOffset, true)
	}

	// Handle upload button click
	if v.uploadBtn.Clicked(gtx) {
		go v.pickFiles()
	}

	// Handle link file button click
	if v.linkFileBtn.Clicked(gtx) {
		if v.app.LinkFileDlg != nil {
			channelID := ""
			if conn != nil {
				channelID = conn.ActiveChannelID
			}
			v.app.LinkFileDlg.Show(func(results []api.UploadResult) {
				if len(results) == 0 || channelID == "" {
					return
				}
				// Directly add as pending uploads (done state)
				v.uploadMu.Lock()
				v.uploadChannelID = channelID
				for _, r := range results {
					r := r
					v.pendingUploads = append(v.pendingUploads, &pendingUpload{
						filename:  r.Original,
						size:      r.Size,
						sent:      r.Size,
						status:    1, // done
						result:    &r,
						startTime: time.Now(),
					})
				}
				v.uploadMu.Unlock()
				// Open upload dialog for sending
				v.app.UploadDlg.Show()
				v.app.Window.Invalidate()
			})
		}
	}

	// Handle mention clicks
	if v.showMentions {
		members := v.getMentionCandidates()
		if len(v.mentionBtns) < len(members) {
			v.mentionBtns = make([]widget.Clickable, len(members)+10)
		}
		for i, m := range members {
			if v.mentionBtns[i].Clicked(gtx) {
				v.insertMention(m.Username)
			}
		}
	}

	// Handle poll button click
	if v.pollBtn.Clicked(gtx) {
		v.showPollBuilder = !v.showPollBuilder
		if v.showPollBuilder {
			if v.pollBuilder == nil {
				v.pollBuilder = NewPollBuilder(v.app)
			} else {
				v.pollBuilder.Reset()
			}
			v.showEmojis = false
			v.showScheduleBuilder = false
		}
	}

	// Handle poll builder create/cancel
	if v.pollBuilder != nil && v.showPollBuilder {
		if v.pollBuilder.cancelBtn.Clicked(gtx) {
			v.showPollBuilder = false
		}
		if v.pollBuilder.createBtn.Clicked(gtx) && v.pollBuilder.IsValid() {
			question := v.pollBuilder.GetQuestion()
			pollType := v.pollBuilder.GetPollType()
			options := v.pollBuilder.GetOptions()
			expiresAt := v.pollBuilder.GetExpiresAt()
			v.showPollBuilder = false

			go func() {
				conn := v.app.Conn()
				if conn == nil {
					return
				}
				poll := &api.PollCreateRequest{
					Question:  question,
					PollType:  pollType,
					Options:   options,
					ExpiresAt: expiresAt,
				}
				_, err := conn.Client.SendMessageFull(conn.ActiveChannelID, "", "", nil, poll)
				if err != nil {
					log.Printf("SendMessageWithPoll error: %v", err)
					v.app.Toasts.Error("Failed to send poll")
				}
			}()
		}
	}

	// Handle schedule button click
	if v.scheduleBtn.Clicked(gtx) {
		v.showScheduleBuilder = !v.showScheduleBuilder
		if v.showScheduleBuilder {
			if v.scheduleBuilder == nil {
				v.scheduleBuilder = NewScheduleBuilder(v.app)
			} else {
				v.scheduleBuilder.Reset()
			}
			v.showEmojis = false
			v.showPollBuilder = false
		}
	}

	// Handle schedule builder create/cancel
	if v.scheduleBuilder != nil && v.showScheduleBuilder {
		if v.scheduleBuilder.cancelBtn.Clicked(gtx) {
			v.showScheduleBuilder = false
		}
		if v.scheduleBuilder.scheduleBtn.Clicked(gtx) && v.scheduleBuilder.hasSelection {
			content := v.editor.Text()
			if content == "" {
				content = v.editor.Text()
			}
			if content != "" {
				scheduledAt := v.scheduleBuilder.selectedTime
				replyToID := v.ReplyToID
				v.showScheduleBuilder = false
				v.editor.SetText("")
				v.ReplyToID = ""
				v.ReplyToContent = ""
				v.ReplyToAuthor = ""

				go func() {
					conn := v.app.Conn()
					if conn == nil {
						return
					}
					_, err := conn.Client.ScheduleMessage(conn.ActiveChannelID, content, replyToID, scheduledAt)
					if err != nil {
						log.Printf("ScheduleMessage error: %v", err)
						v.app.Toasts.Error("Failed to schedule message")
					}
					// Refresh list
					if v.scheduleBuilder != nil {
						v.scheduleBuilder.loaded = false
						v.scheduleBuilder.LoadScheduled()
					}
				}()
			}
		}
	}

	// Handle emoji picker toggle
	if v.emojiBtn.Clicked(gtx) {
		v.showEmojis = !v.showEmojis
		if v.showEmojis {
			v.emojiCategoryIdx = 1 // Default to Smileys
			v.showPollBuilder = false
			v.showScheduleBuilder = false
		}
	}

	// Handle emoji click (insert into editor)
	emojis := v.getEmojis()
	if len(v.emojiClickBtns) < len(emojis) {
		v.emojiClickBtns = make([]widget.Clickable, len(emojis)+10)
	}
	for i, e := range emojis {
		if v.emojiClickBtns[i].Clicked(gtx) {
			v.editor.Insert(":" + e.Name + ":")
			v.showEmojis = false
		}
	}

	// Handle Unicode emoji clicks
	uniIdx := 0
	for _, cat := range UnicodeEmojiCategories {
		for _, emoji := range cat.Emojis {
			if uniIdx < len(v.unicodeEmojiBtns) && v.unicodeEmojiBtns[uniIdx].Clicked(gtx) {
				v.editor.Insert(emoji)
				v.showEmojis = false
			}
			uniIdx++
		}
	}

	// Handle category tab clicks
	for i := range v.emojiCatBtns {
		if i < len(v.emojiCatBtns) && v.emojiCatBtns[i].Clicked(gtx) {
			v.emojiCategoryIdx = i
		}
	}

	// Handle upload bar click → open popup
	if v.uploadBarBtn.Clicked(gtx) {
		v.app.UploadDlg.Show()
	}

	// Handle auto-send signal from upload goroutine
	if v.autoSendPending {
		v.autoSendPending = false
		v.app.UploadDlg.doSend(v)
	}

	// Handle action button clicks
	for i, msg := range messages {
		if v.actions[i].copyBtn.Clicked(gtx) {
			copyToClipboard(msg.Content)
		}
		if v.actions[i].bookmarkBtn.Clicked(gtx) {
			conn := v.app.Conn()
			if conn != nil && v.app.Bookmarks != nil {
				if v.app.Bookmarks.IsBookmarked(msg.ID, conn.URL) {
					v.app.Bookmarks.Remove(msg.ID, conn.URL)
				} else {
					channelName := ""
					for _, ch := range conn.Channels {
						if ch.ID == conn.ActiveChannelID {
							channelName = ch.Name
							break
						}
					}
					authorName := ""
					if msg.Author != nil {
						authorName = v.app.ResolveUserName(msg.Author)
					}
					content := msg.Content
					if len(content) > 200 {
						content = content[:200]
					}
					v.app.Bookmarks.Add(store.StoredBookmark{
						ID:          msg.ID,
						ServerURL:   conn.URL,
						ChannelID:   msg.ChannelID,
						ChannelName: channelName,
						Content:     content,
						AuthorID:    msg.UserID,
						AuthorName:  authorName,
						CreatedAt:   msg.CreatedAt,
					})
				}
				go v.app.Bookmarks.Save()
			}
		}
		// Click on "(edited)" — show edit history
		if v.actions[i].editHistoryBtn.Clicked(gtx) {
			msgID := msg.ID
			v.editHistoryMsgID = msgID
			v.editHistory = nil
			v.showEditHistory = true
			go func() {
				if conn := v.app.Conn(); conn != nil {
					edits, err := conn.Client.GetMessageEditHistory(msgID)
					if err != nil {
						log.Printf("GetMessageEditHistory error: %v", err)
						return
					}
					v.editHistory = edits
					v.app.Window.Invalidate()
				}
			}()
		}
		if v.actions[i].nameBtn.Clicked(gtx) || v.actions[i].avatarBtn.Clicked(gtx) {
			if msg.Author != nil {
				v.app.UserPopup.Show(msg.UserID, v.app.ResolveUserName(msg.Author))
			}
		}
		// P2P download click
		if v.actions[i].p2pBtn.Clicked(gtx) {
			if info := parseP2PLink(msg.Content); info != nil && conn != nil && conn.P2P != nil {
				senderID := info.senderID
				transferID := info.transferID
				fileName := info.fileName
				go func() {
					savePath := saveFileDialog(fileName)
					if savePath == "" {
						return
					}
					conn.P2P.RequestDownload(senderID, transferID, savePath)
					v.app.Window.Invalidate()
				}()
			}
		}
		// Download All — download all game server attachments
		if v.actions[i].downloadAllBtn.Clicked(gtx) {
			attachments := msg.Attachments
			serverURL := ""
			token := ""
			if conn != nil {
				serverURL = conn.URL
				token = conn.Client.GetAccessToken()
			}
			go func() {
				dir, err := pickDirectory()
				if err != nil || dir == "" || serverURL == "" {
					return
				}
				v.startDirectoryDownload(attachments, serverURL, token, dir)
			}()
		}
		for j := 0; j < v.actions[i].mentions.N && j < 4; j++ {
			if v.actions[i].mentions.Btns[j].Clicked(gtx) {
				uid := v.actions[i].mentions.UserIDs[j]
				if uid != "" {
					name := ""
					if conn != nil {
						for _, u := range conn.Users {
							if u.ID == uid {
								name = v.app.ResolveUserName(&u)
								break
							}
						}
					}
					v.app.UserPopup.Show(uid, name)
				}
			}
		}
		if v.actions[i].replyBtn.Clicked(gtx) {
			author := v.app.ResolveUserName(msg.Author)
			v.ReplyToID = msg.ID
			v.ReplyToContent = msg.Content
			v.ReplyToAuthor = author
		}
		if v.actions[i].editBtn.Clicked(gtx) && msg.UserID == userID {
			v.EditingID = msg.ID
			v.editOriginal = msg.Content
			v.editor.SetText(msg.Content)
		}
		canDeleteThis := msg.UserID == userID || myPerms&(api.PermManageMessages|api.PermAdmin) != 0
		if v.actions[i].deleteBtn.Clicked(gtx) && canDeleteThis {
			msgID := msg.ID
			v.app.ConfirmDlg.Show("Delete Message", "Are you sure you want to delete this message?", func() {
				if conn := v.app.Conn(); conn != nil {
					if err := conn.Client.DeleteMessage(msgID); err != nil {
						log.Printf("DeleteMessage error: %v", err)
						v.app.Toasts.Error("Failed to delete message")
					}
				}
			})
		}
		if v.actions[i].pinBtn.Clicked(gtx) {
			msgID := msg.ID
			pinned := !msg.IsPinned
			go func() {
				if conn := v.app.Conn(); conn != nil {
					if err := conn.Client.PinMessage(msgID, pinned); err != nil {
						log.Printf("PinMessage error: %v", err)
						v.app.Toasts.Error("Failed to pin message")
					}
				}
			}()
		}
		if v.actions[i].hideBtn.Clicked(gtx) {
			msgID := msg.ID
			hidden := !msg.IsHidden
			go func() {
				if conn := v.app.Conn(); conn != nil {
					if err := conn.Client.HideMessage(msgID, hidden); err != nil {
						log.Printf("HideMessage error: %v", err)
					}
				}
			}()
		}

		// Handle reaction emoji picker toggle
		if v.actions[i].reactEmojiBtn.Clicked(gtx) {
			if v.reactPickerMsgIdx == i {
				v.reactPickerMsgIdx = -1
			} else {
				v.reactPickerMsgIdx = i
			}
		}

		// Handle reaction clicks
		if v.actions[i].thumbUpBtn.Clicked(gtx) {
			msgID := msg.ID
			go func() {
				if c := v.app.Conn(); c != nil {
					c.Client.ToggleReaction(msgID, "\U0001f44d")
				}
			}()
		}
		if v.actions[i].thumbDownBtn.Clicked(gtx) {
			msgID := msg.ID
			go func() {
				if c := v.app.Conn(); c != nil {
					c.Client.ToggleReaction(msgID, "\U0001f44e")
				}
			}()
		}
		for j, r := range msg.Reactions {
			if j >= len(v.actions[i].reactionBtns) {
				break
			}
			if v.actions[i].reactionBtns[j].Clicked(gtx) {
				msgID := msg.ID
				emoji := r.Emoji
				go func() {
					if c := v.app.Conn(); c != nil {
						c.Client.ToggleReaction(msgID, emoji)
					}
				}()
			}
		}

		// Handle reaction picker emoji clicks
		if v.reactPickerMsgIdx == i {
			for j := range v.reactPickerBtns {
				if v.reactPickerBtns[j].Clicked(gtx) {
					emoji := reactionEmojis[j]
					msgID := msg.ID
					v.reactPickerMsgIdx = -1
					go func() {
						if c := v.app.Conn(); c != nil {
							c.Client.ToggleReaction(msgID, emoji)
						}
					}()
					break
				}
			}
		}

		// Handle poll option clicks
		if msg.Poll != nil {
			for j, opt := range msg.Poll.Options {
				if j >= len(v.actions[i].pollOptBtns) {
					break
				}
				if v.actions[i].pollOptBtns[j].Clicked(gtx) {
					pollID := msg.Poll.ID
					optionID := opt.ID
					go func() {
						if c := v.app.Conn(); c != nil {
							c.Client.VotePoll(pollID, optionID)
						}
					}()
				}
			}
		}

		// Handle attachment clicks — open images in viewer, download others
		for j, att := range msg.Attachments {
			if j >= len(v.actions[i].attBtns) {
				break
			}
			if v.actions[i].attBtns[j].Clicked(gtx) {
				if conn := v.app.Conn(); conn != nil {
					fileURL := conn.URL + att.URL
					fname := att.Filename
					tok := conn.Client.GetAccessToken()
					isImage := strings.HasPrefix(att.MimeType, "image/")
					isVideo := isVideoMIME(att.MimeType)
					if isVideo {
						v.app.VideoPlayer.Play(fileURL, fname)
					} else if isImage {
						go openURL(fileURL)
					} else {
						v.app.SaveDlg.Show(fname, func(savePath string) {
							v.startDownload(fileURL, savePath, fname, tok)
						})
					}
				}
			}
			// Video save button
			if j < len(v.actions[i].vidSaveBtns) && v.actions[i].vidSaveBtns[j].Clicked(gtx) {
				if isVideoMIME(att.MimeType) {
					if conn := v.app.Conn(); conn != nil {
						fileURL := conn.URL + att.URL
						fname := att.Filename
						tok := conn.Client.GetAccessToken()
						v.app.SaveDlg.Show(fname, func(savePath string) {
							v.startDownload(fileURL, savePath, fname, tok)
						})
					}
				}
			}
		}
	}

	// Cancel reply/edit
	if v.cancelReply.Clicked(gtx) {
		v.ReplyToID = ""
	}
	if v.cancelEdit.Clicked(gtx) {
		v.EditingID = ""
		v.editor.SetText("")
	}

	// Scroll-to-load: detect scroll near top
	if v.list.Position.First == 0 && v.list.Position.Offset == 0 && len(messages) > 0 && !v.loadingOlder && !v.noMoreOlder {
		v.loadOlderMessages()
	}

	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(v.app.Theme.Material, "# "+channelName)
						lbl.Color = ColorText
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if channelTopic == "" {
							return layout.Dimensions{}
						}
						return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, channelTopic)
							lbl.Color = ColorTextDim
							return lbl.Layout(gtx)
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.searchBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								clr := ColorTextDim
								if v.showSearch {
									clr = ColorAccent
								}
								return layoutIcon(gtx, IconSearch, 18, clr)
							})
						})
					}),
				)
			})
		}),

		// Search bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showSearch {
				return layout.Dimensions{}
			}
			return v.layoutSearchBar(gtx)
		}),

		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),

		// Pinned message bar (always visible when pins exist)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(v.pinnedMsgs) == 0 {
				return layout.Dimensions{}
			}
			if v.pinBarBtn.Clicked(gtx) {
				v.pinExpanded = !v.pinExpanded
				v.showPins = v.pinExpanded
				// Mark as seen + persist
				if len(v.pinnedMsgs) > 0 {
					v.pinSeenID = v.pinnedMsgs[0].ID
					go func() {
						if conn := v.app.Conn(); conn != nil {
							store.SetPinSeenID(v.app.PublicKey, conn.URL, conn.ActiveChannelID, v.pinSeenID)
						}
					}()
				}
				if v.showPins {
					go func() {
						if conn := v.app.Conn(); conn != nil {
							pins, err := conn.Client.GetPinnedMessages(conn.ActiveChannelID)
							if err == nil {
								v.pinnedMsgs = pins
								v.app.Window.Invalidate()
							}
						}
					}()
				}
			}
			latest := v.pinnedMsgs[0]
			if v.pinUnpinBtn.Clicked(gtx) {
				msgID := latest.ID
				go func() {
					if conn := v.app.Conn(); conn != nil {
						if err := conn.Client.PinMessage(msgID, false); err != nil {
							log.Printf("Unpin error: %v", err)
						}
					}
				}()
			}
			if v.pinReplyBtn.Clicked(gtx) {
				v.ReplyToID = latest.ID
				v.ReplyToContent = latest.Content
				if latest.Author != nil {
					v.ReplyToAuthor = v.app.ResolveUserName(latest.Author)
				}
			}
			if v.pinCopyBtn.Clicked(gtx) {
				copyToClipboard(latest.Content)
			}
			if v.pinThumbUpBtn.Clicked(gtx) {
				msgID := latest.ID
				go func() {
					if conn := v.app.Conn(); conn != nil {
						conn.Client.ToggleReaction(msgID, "\U0001f44d")
					}
				}()
			}
			if v.pinThumbDownBtn.Clicked(gtx) {
				msgID := latest.ID
				go func() {
					if conn := v.app.Conn(); conn != nil {
						conn.Client.ToggleReaction(msgID, "\U0001f44e")
					}
				}()
			}
			if v.pinEditBtn.Clicked(gtx) {
				if latest.UserID == userID {
					v.EditingID = latest.ID
					v.editor.SetText(latest.Content)
				}
			}
			if v.pinDeleteBtn.Clicked(gtx) {
				msgID := latest.ID
				v.app.ConfirmDlg.ShowConfirm("Delete", "Delete this pinned message?", func() {
					go func() {
						if conn := v.app.Conn(); conn != nil {
							conn.Client.DeleteMessage(msgID)
						}
					}()
				})
			}
			// Clicks on attachments in the pin bar
			for j := range latest.Attachments {
				if j >= len(v.pinAttBtns) {
					break
				}
				if v.pinAttBtns[j].Clicked(gtx) {
					att := latest.Attachments[j]
					if conn := v.app.Conn(); conn != nil {
						fileURL := conn.URL + att.URL
						tok := conn.Client.GetAccessToken()
						fname := att.Filename
						if isVideoMIME(att.MimeType) {
							v.app.VideoPlayer.Play(fileURL, tok)
						} else if strings.HasPrefix(att.MimeType, "image/") {
							go openURL(fileURL)
						} else {
							v.app.SaveDlg.Show(fname, func(savePath string) {
								v.startDownload(fileURL, savePath, fname, tok)
							})
						}
					}
				}
			}
			// Click on URL link in the pin bar
			if v.pinLinkBtn.Clicked(gtx) {
				url := findURLInText(latest.Content)
				if url != "" {
					go openURL(url)
				}
			}
			return v.layoutPinBar(gtx)
		}),

		// Expanded pinned messages panel
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showPins || len(v.pinnedMsgs) < 2 {
				return layout.Dimensions{}
			}
			// Click handling for pinned list actions
			canPin := myPerms&(api.PermManageMessages|api.PermAdmin) != 0
			for i := range v.pinsListActs {
				if i >= len(v.pinnedMsgs) {
					break
				}
				msg := v.pinnedMsgs[i]
				act := &v.pinsListActs[i]
				if act.unpinBtn.Clicked(gtx) && canPin {
					msgID := msg.ID
					go func() {
						if c := v.app.Conn(); c != nil {
							c.Client.PinMessage(msgID, false)
						}
					}()
				}
				if act.replyBtn.Clicked(gtx) {
					v.ReplyToID = msg.ID
					v.ReplyToContent = msg.Content
					if msg.Author != nil {
						v.ReplyToAuthor = v.app.ResolveUserName(msg.Author)
					}
				}
				if act.copyBtn.Clicked(gtx) {
					copyToClipboard(msg.Content)
				}
				if act.thumbUpBtn.Clicked(gtx) {
					msgID := msg.ID
					go func() {
						if c := v.app.Conn(); c != nil {
							c.Client.ToggleReaction(msgID, "\U0001f44d")
						}
					}()
				}
				if act.thumbDownBtn.Clicked(gtx) {
					msgID := msg.ID
					go func() {
						if c := v.app.Conn(); c != nil {
							c.Client.ToggleReaction(msgID, "\U0001f44e")
						}
					}()
				}
				if act.editBtn.Clicked(gtx) {
					if msg.UserID == userID {
						v.EditingID = msg.ID
						v.editor.SetText(msg.Content)
					}
				}
				if act.deleteBtn.Clicked(gtx) {
					isOwn := msg.UserID == userID
					canDel := isOwn || myPerms&(api.PermManageMessages|api.PermAdmin) != 0
					if canDel {
						msgID := msg.ID
						v.app.ConfirmDlg.ShowConfirm("Delete", "Delete this pinned message?", func() {
							go func() {
								if c := v.app.Conn(); c != nil {
									c.Client.DeleteMessage(msgID)
								}
							}()
						})
					}
				}
				if act.linkPreviewBtn.Clicked(gtx) {
					if msg.LinkPreview != nil && msg.LinkPreview.URL != "" {
						go openURL(msg.LinkPreview.URL)
					}
				}
				for j := range msg.Attachments {
					if j >= len(act.attBtns) {
						break
					}
					if act.attBtns[j].Clicked(gtx) {
						att := msg.Attachments[j]
						if c := v.app.Conn(); c != nil {
							fileURL := c.URL + att.URL
							tok := c.Client.GetAccessToken()
							fname := att.Filename
							if isVideoMIME(att.MimeType) {
								v.app.VideoPlayer.Play(fileURL, tok)
							} else if strings.HasPrefix(att.MimeType, "image/") {
								go openURL(fileURL)
							} else {
								v.app.SaveDlg.Show(fname, func(savePath string) {
									v.startDownload(fileURL, savePath, fname, tok)
								})
							}
						}
					}
				}
			}
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.layoutPinnedMessages(gtx)
			})
		}),

		// Upload panel
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutUploadPanel(gtx)
		}),

		// Download panel
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutDownloadPanel(gtx)
		}),

		// P2P transfer panel
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutP2PPanel(gtx)
		}),

		// Connection status bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			conn := v.app.Conn()
			if conn == nil || conn.WS == nil || conn.WS.IsConnected() {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, "Reconnecting...")
				lbl.Color = color.NRGBA{R: 255, G: 180, B: 0, A: 255}
				return lbl.Layout(gtx)
			})
		}),

		// Typing indicator
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutTypingIndicator(gtx)
		}),

		// Messages (or search results)
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if v.showPins && len(v.pinnedMsgs) > 0 {
				// Darkened overlay over messages when pinned panel is open
				overlay := color.NRGBA{A: 60}
				paint.FillShape(gtx.Ops, overlay, clip.Rect{Max: gtx.Constraints.Max}.Op())
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}
			if v.showSearch && v.searchQuery != "" {
				return v.layoutSearchResults(gtx, userID, myPerms, usernames, usernameToID, myUsername)
			}
			if len(messages) == 0 {
				return layoutCentered(gtx, v.app.Theme, "No messages yet", ColorTextDim)
			}

			// Scroll to bottom button
			if v.scrollBottomBtn.Clicked(gtx) {
				v.list.Position.First = len(messages)
				v.list.Position.Offset = 0
				v.list.Position.BeforeEnd = false
			}

			return layout.Stack{Alignment: layout.SE}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					// +1 item for "beginning" indicator at top
					listLen := len(messages)
					if v.noMoreOlder {
						listLen++
					}
					return material.List(v.app.Theme.Material, &v.list).Layout(gtx, listLen, func(gtx layout.Context, idx int) layout.Dimensions {
						if v.noMoreOlder && idx == 0 {
							return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceSides}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, "Beginning of conversation")
										lbl.Color = ColorTextDim
										return lbl.Layout(gtx)
									}),
								)
							})
						}
						msgIdx := idx
						if v.noMoreOlder {
							msgIdx = idx - 1
						}
						msg := messages[msgIdx]

						grouped := false
						if msgIdx > 0 {
							prev := messages[msgIdx-1]
							if prev.UserID == msg.UserID && msg.ReplyToID == nil &&
								msg.CreatedAt.Sub(prev.CreatedAt) < 5*time.Minute {
								grouped = true
							}
						}

						isOwn := msg.UserID == userID
						return v.layoutMessage(gtx, msg, grouped, isOwn, msgIdx, myUsername, usernames, usernameToID)
					})
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					if !v.list.Position.BeforeEnd {
						return layout.Dimensions{}
					}
					return layout.Inset{Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return v.scrollBottomBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							sz := gtx.Dp(32)
							bg := ColorAccent
							if v.scrollBottomBtn.Hovered() {
								bg = ColorAccentHover
							}
							rr := sz / 2
							paint.FillShape(gtx.Ops, bg, clip.RRect{
								Rect: image.Rect(0, 0, sz, sz),
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Stack{Alignment: layout.Center}.Layout(gtx,
								layout.Stacked(func(gtx layout.Context) layout.Dimensions {
									return layout.Dimensions{Size: image.Pt(sz, sz)}
								}),
								layout.Stacked(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, "v")
									lbl.Color = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
									return lbl.Layout(gtx)
								}),
							)
						})
					})
				}),
			)
		}),

		// Reply preview
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if v.ReplyToID == "" {
				return layout.Dimensions{}
			}
			return v.layoutReplyPreview(gtx)
		}),

		// Edit indicator
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if v.EditingID == "" {
				return layout.Dimensions{}
			}
			return v.layoutEditIndicator(gtx)
		}),

		// Slow mode error
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if v.slowModeErr == "" || time.Now().After(v.slowModeUntil) {
				v.slowModeErr = ""
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, v.slowModeErr)
				lbl.Color = color.NRGBA{R: 255, G: 80, B: 80, A: 255}
				return lbl.Layout(gtx)
			})
		}),

		// Mention autocomplete popup
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showMentions {
				return layout.Dimensions{}
			}
			return v.layoutMentionPopup(gtx)
		}),

		// Emoji picker popup
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showEmojis {
				return layout.Dimensions{}
			}
			return v.layoutEmojiPicker(gtx)
		}),

		// Poll builder popup
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showPollBuilder || v.pollBuilder == nil {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.pollBuilder.Layout(gtx)
			})
		}),

		// Schedule builder popup
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !v.showScheduleBuilder || v.scheduleBuilder == nil {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return v.scheduleBuilder.Layout(gtx)
			})
		}),

		// Formatting toolbar (B, I, S, </>)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutFormattingToolbar(gtx)
		}),

		// Input row (upload button + editor + emoji button)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(12), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.End}.Layout(gtx,
					// Upload button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutCircleIconBtn(gtx, &v.uploadBtn, IconUpload, false)
					}),
					// Link file button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutCircleIconBtn(gtx, &v.linkFileBtn, IconLink, false)
					}),
					// Editor
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Background{}.Layout(gtx,
								func(gtx layout.Context) layout.Dimensions {
									bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
									rr := gtx.Dp(8)
									paint.FillShape(gtx.Ops, ColorInput, clip.RRect{
										Rect: bounds,
										NE:   rr, NW: rr, SE: rr, SW: rr,
									}.Op(gtx.Ops))
									return layout.Dimensions{Size: bounds.Max}
								},
								func(gtx layout.Context) layout.Dimensions {
									maxH := gtx.Dp(200)
									if gtx.Constraints.Max.Y > maxH {
										gtx.Constraints.Max.Y = maxH
									}
									return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										hint := "Message #" + channelName
										if v.EditingID != "" {
											hint = "Editing message..."
										}
										ed := material.Editor(v.app.Theme.Material, &v.editor, hint)
										ed.Color = ColorText
										ed.HintColor = ColorTextDim
										txt := v.editor.Text()
										if strings.Count(txt, "\n") >= 7 {
											// Sync scrollbar ke kurzoru
											start, _ := v.editor.Selection()
											runes := []rune(txt)
											if start > len(runes) {
												start = len(runes)
											}
											cursorLine := strings.Count(string(runes[:start]), "\n")
											totalLines := strings.Count(txt, "\n")
											lineH := gtx.Dp(20)
											cursorY := cursorLine * lineH
											viewH := maxH - gtx.Dp(24)
											// Prevent ScrollToEnd override — set BeforeEnd
											v.editorList.Position.BeforeEnd = cursorLine < totalLines
											v.editorList.Position.First = 0
											if viewH > 0 {
												off := v.editorList.Position.Offset
												if cursorY < off {
													v.editorList.Position.Offset = cursorY
												} else if cursorY+lineH > off+viewH {
													v.editorList.Position.Offset = cursorY + lineH - viewH
												}
											}
											lst := material.List(v.app.Theme.Material, &v.editorList)
											return lst.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
												return ed.Layout(gtx)
											})
										}
										return ed.Layout(gtx)
									})
								},
							)
						})
					}),
					// Emoji button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutCircleIconBtn(gtx, &v.emojiBtn, IconEmoji, false)
					}),
					// Poll button (+)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutCircleIconBtn(gtx, &v.pollBtn, IconAdd, false)
					}),
					// Schedule button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutCircleIconBtn(gtx, &v.scheduleBtn, IconSchedule, false)
					}),
				)
			})
		}),
	)

	// Edit history overlay (above other content)
	if v.showEditHistory {
		v.layoutEditHistoryOverlay(gtx)
	}

	return dims
}

// layoutMessageCompact — IRC-style compact message: [HH:MM] username: content
func (v *MessageView) layoutMessageCompact(gtx layout.Context, msg api.Message, isOwn bool, idx int, myUsername string, usernames map[string]bool, usernameToID map[string]string) layout.Dimensions {
	serverURL := ""
	conn := v.app.Conn()
	if conn != nil {
		serverURL = conn.URL
	}

	// Hover tracking + double-click reply (same as normal mode)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &v.actions[idx].rowHovered,
			Kinds:  pointer.Enter | pointer.Leave | pointer.Press,
		})
		if !ok {
			break
		}
		switch ev.(type) {
		case pointer.Event:
			pe := ev.(pointer.Event)
			if pe.Kind == pointer.Enter {
				v.actions[idx].rowHovered = true
			} else if pe.Kind == pointer.Leave {
				v.actions[idx].rowHovered = false
			} else if pe.Kind == pointer.Press {
				now := gtx.Now
				if now.Sub(v.actions[idx].lastPress) < 400*time.Millisecond && v.ReplyToID == "" && v.EditingID == "" {
					v.ReplyToID = msg.ID
					v.ReplyToContent = msg.Content
					if msg.Author != nil {
						v.ReplyToAuthor = v.app.ResolveUserName(msg.Author)
					}
				}
				v.actions[idx].lastPress = now
			}
		}
	}
	hovered := v.actions[idx].rowHovered

	username := "?"
	if msg.Author != nil {
		username = v.app.ResolveUserName(msg.Author)
	}
	nameColor := UserColor(username)
	if conn != nil && msg.Author != nil {
		nameColor = v.app.GetUserRoleColor(conn, msg.UserID, username)
	}
	timeStr := FormatTime(msg.CreatedAt)

	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		macro := op.Record(gtx.Ops)
		dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Main row: [time] name: content + action btns
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
					// Timestamp
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, timeStr)
						lbl.Color = ColorTextDim
						return lbl.Layout(gtx)
					}),
					// Username
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.actions[idx].nameBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(v.app.Theme.Material, username+":")
								lbl.Color = nameColor
								lbl.Font.Weight = 700
								return lbl.Layout(gtx)
							})
						})
					}),
					// Content
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							if msg.IsHidden {
								lbl := material.Caption(v.app.Theme.Material, "[hidden]")
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							}
							content := msg.Content
							emojis := v.getEmojis()
							dims := layoutMessageContent(gtx, v.app.Theme, content, emojis, &v.actions[idx].links, &v.actions[idx].mentions, usernameToID, usernames, &v.actions[idx].textSels, v.app, serverURL)
							if msg.UpdatedAt != nil {
								off := op.Offset(image.Pt(0, dims.Size.Y)).Push(gtx.Ops)
								editDims := v.layoutEditedLabel(gtx, idx)
								off.Pop()
								if editDims.Size.X > dims.Size.X {
									dims.Size.X = editDims.Size.X
								}
								dims.Size.Y += editDims.Size.Y
							}
							return dims
						})
					}),
				)
			}),
			// Attachments
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if len(msg.Attachments) == 0 {
					return layout.Dimensions{}
				}
				return layout.Inset{Left: unit.Dp(40)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutAttachments(gtx, idx, msg)
				})
			}),
			// Reactions
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if len(msg.Reactions) == 0 {
					return layout.Dimensions{}
				}
				return layout.Inset{Left: unit.Dp(40)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutReactions(gtx, idx, msg)
				})
			}),
		)
		call := macro.Stop()

		// Hover background
		if hovered {
			paint.FillShape(gtx.Ops, ColorHover, clip.Rect{Max: dims.Size}.Op())
		}
		call.Add(gtx.Ops)

		// Action buttons overlay (top-right)
		if hovered {
			actMacro := op.Record(gtx.Ops)
			actDims := v.layoutActionBtns(gtx, idx, isOwn, msg.IsPinned, msg.IsHidden, msg.ID)
			actCall := actMacro.Stop()
			offX := dims.Size.X - actDims.Size.X
			if offX < 0 {
				offX = 0
			}
			st := op.Offset(image.Pt(offX, 0)).Push(gtx.Ops)
			actCall.Add(gtx.Ops)
			st.Pop()
		}

		// Pointer area
		pr := pointer.PassOp{}.Push(gtx.Ops)
		rect := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
		event.Op(gtx.Ops, &v.actions[idx].rowHovered)
		rect.Pop()
		pr.Pop()

		return dims
	})
}

func (v *MessageView) layoutMessage(gtx layout.Context, msg api.Message, grouped, isOwn bool, idx int, myUsername string, usernames map[string]bool, usernameToID map[string]string) layout.Dimensions {
	// Compact mode — IRC styl
	if v.app.Theme.CompactMode {
		return v.layoutMessageCompact(gtx, msg, isOwn, idx, myUsername, usernames, usernameToID)
	}

	topPad := unit.Dp(8)
	if grouped {
		topPad = unit.Dp(1)
	}

	serverURL := ""
	if c := v.app.Conn(); c != nil {
		serverURL = c.URL
	}

	// Mention highlight — subtle tint if message mentions the current user
	isMentioned := myUsername != "" && strings.Contains(strings.ToLower(msg.Content), "@"+strings.ToLower(myUsername))

	// Hover tracking + double-click reply via pointer events (does not block nested interactions)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &v.actions[idx].rowHovered,
			Kinds:  pointer.Enter | pointer.Leave | pointer.Press,
		})
		if !ok {
			break
		}
		switch ev.(type) {
		case pointer.Event:
			pe := ev.(pointer.Event)
			if pe.Kind == pointer.Enter {
				v.actions[idx].rowHovered = true
			} else if pe.Kind == pointer.Leave {
				v.actions[idx].rowHovered = false
			} else if pe.Kind == pointer.Press {
				now := gtx.Now
				if now.Sub(v.actions[idx].lastPress) < 400*time.Millisecond && v.ReplyToID == "" && v.EditingID == "" {
					// Double-click → reply
					v.ReplyToID = msg.ID
					v.ReplyToContent = msg.Content
					if msg.Author != nil {
						v.ReplyToAuthor = v.app.ResolveUserName(msg.Author)
					}
				}
				v.actions[idx].lastPress = now
			}
		}
	}
	hovered := v.actions[idx].rowHovered

	return layout.Inset{Top: topPad, Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// First layout content into a macro so we know the actual size
		macro := op.Record(gtx.Ops)
		dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Reply reference (click -> open thread)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if msg.ReplyTo == nil {
					return layout.Dimensions{}
				}
				if msg.ReplyToID != nil && v.actions[idx].replyRefBtn.Clicked(gtx) {
					v.app.ThreadView.Open(*msg.ReplyToID)
				}
				return v.actions[idx].replyRefBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutReplyRef(gtx, msg.ReplyTo)
				})
			}),
			// Message row
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					// Avatar (36px)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						size := gtx.Dp(36)
						if grouped {
							gtx.Constraints.Min.X = size
							gtx.Constraints.Max.X = size
							lbl := material.Caption(v.app.Theme.Material, FormatTime(msg.CreatedAt))
							if hovered {
								lbl.Color = ColorTextDim
							} else {
								lbl.Color = color.NRGBA{A: 0} // invisible but takes up space
							}
							return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return lbl.Layout(gtx)
							})
						}

						username := v.app.ResolveUserName(msg.Author)
						avatarURL := ""
						if msg.Author != nil {
							avatarURL = msg.Author.AvatarURL
						}
						return v.actions[idx].avatarBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutAvatar(gtx, v.app, username, avatarURL, 36)
						})
					}),

					// Content
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							if grouped {
								hasGroupedContent := strings.TrimSpace(msg.Content) != "" || (msg.IsHidden && strings.TrimSpace(msg.Content) == "")

								groupedBody := func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										// Text content (only if it exists)
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if !hasGroupedContent {
												return layout.Dimensions{}
											}
											// Placeholder for hidden messages (automod)
											if msg.IsHidden && strings.TrimSpace(msg.Content) == "" {
												lbl := material.Body2(v.app.Theme.Material, "This message was hidden by auto-moderation")
												lbl.Color = ColorTextDim
												lbl.Font.Style = font.Italic
												return lbl.Layout(gtx)
											}
											if p2pInfo := parseP2PLink(msg.Content); p2pInfo != nil {
												return v.layoutP2PBlock(gtx, p2pInfo, idx, isOwn)
											}
											content := msg.Content
											emojis := v.getEmojis()
											dims := layoutMessageContent(gtx, v.app.Theme, content, emojis, &v.actions[idx].links, &v.actions[idx].mentions, usernameToID, usernames, &v.actions[idx].textSels, v.app, serverURL)
											if msg.UpdatedAt != nil {
												off := op.Offset(image.Pt(0, dims.Size.Y)).Push(gtx.Ops)
												editDims := v.layoutEditedLabel(gtx, idx)
												off.Pop()
												if editDims.Size.X > dims.Size.X {
													dims.Size.X = editDims.Size.X
												}
												dims.Size.Y += editDims.Size.Y
											}
											return dims
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if len(msg.Attachments) == 0 {
												return layout.Dimensions{}
											}
											return v.layoutAttachments(gtx, idx, msg)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if msg.LinkPreview == nil {
												return layout.Dimensions{}
											}
											return v.layoutLinkPreview(gtx, msg, idx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											ytURL := findYouTubeURLInText(msg.Content)
											if ytURL == "" {
												return layout.Dimensions{}
											}
											return layoutYouTubeEmbed(gtx, v.app, msg.ID, ytURL)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if len(msg.Reactions) == 0 {
												return layout.Dimensions{}
											}
											return v.layoutReactions(gtx, idx, msg)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return v.layoutReactionPicker(gtx, idx)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if msg.ReplyCount <= 0 {
												return layout.Dimensions{}
											}
											return v.layoutReplyCount(gtx, idx, msg)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if msg.Poll == nil {
												return layout.Dimensions{}
											}
											return v.layoutPoll(gtx, idx, msg)
										}),
									)
								}

								return groupedBody(gtx)
								return groupedBody(gtx)
							}

							hasContent := strings.TrimSpace(msg.Content) != "" || (msg.IsHidden && strings.TrimSpace(msg.Content) == "")

							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Username + date row (+ action btns if message has no text)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Alignment: layout.End}.Layout(gtx,
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return layout.Flex{}.Layout(gtx,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													username := v.app.ResolveUserName(msg.Author)
													return v.actions[idx].nameBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														lbl := material.Body2(v.app.Theme.Material, username)
														if conn := v.app.Conn(); conn != nil {
															lbl.Color = v.app.GetUserRoleColor(conn, msg.UserID, username)
														} else {
															lbl.Color = UserColor(username)
														}
														lbl.Font.Weight = 600
														if v.actions[idx].nameBtn.Hovered() {
															lbl.Color = ColorAccentHover
														}
														return lbl.Layout(gtx)
													})
												}),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														lbl := material.Caption(v.app.Theme.Material, FormatDateTime(msg.CreatedAt))
														lbl.Color = ColorTextDim
														return lbl.Layout(gtx)
													})
												}),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													if !msg.IsPinned {
														return layout.Dimensions{}
													}
													return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														lbl := material.Caption(v.app.Theme.Material, "pinned")
														lbl.Color = ColorAccent
														return lbl.Layout(gtx)
													})
												}),
											)
										}),
									)
								}),
								// Content + action btns (only if there is text content)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if !hasContent {
										return layout.Dimensions{}
									}
									return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										// Placeholder for hidden messages (automod)
										if msg.IsHidden && strings.TrimSpace(msg.Content) == "" {
											lbl := material.Body2(v.app.Theme.Material, "This message was hidden by auto-moderation")
											lbl.Color = ColorTextDim
											lbl.Font.Style = font.Italic
											return lbl.Layout(gtx)
										}
										if p2pInfo := parseP2PLink(msg.Content); p2pInfo != nil {
											return v.layoutP2PBlock(gtx, p2pInfo, idx, isOwn)
										}
										content := msg.Content
										emojis := v.getEmojis()
										dims := layoutMessageContent(gtx, v.app.Theme, content, emojis, &v.actions[idx].links, &v.actions[idx].mentions, usernameToID, usernames, &v.actions[idx].textSels, v.app, serverURL)
										if msg.UpdatedAt != nil {
											off := op.Offset(image.Pt(0, dims.Size.Y)).Push(gtx.Ops)
											editDims := v.layoutEditedLabel(gtx, idx)
											off.Pop()
											if editDims.Size.X > dims.Size.X {
												dims.Size.X = editDims.Size.X
											}
											dims.Size.Y += editDims.Size.Y
										}
										return dims
									})
								}),
								// Attachments
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if len(msg.Attachments) == 0 {
										return layout.Dimensions{}
									}
									return v.layoutAttachments(gtx, idx, msg)
								}),
								// Link preview
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if msg.LinkPreview == nil {
										return layout.Dimensions{}
									}
									return v.layoutLinkPreview(gtx, msg, idx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									ytURL := findYouTubeURLInText(msg.Content)
									if ytURL == "" {
										return layout.Dimensions{}
									}
									return layoutYouTubeEmbed(gtx, v.app, msg.ID, ytURL)
								}),
								// Reactions
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if len(msg.Reactions) == 0 {
										return layout.Dimensions{}
									}
									return v.layoutReactions(gtx, idx, msg)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return v.layoutReactionPicker(gtx, idx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if msg.ReplyCount <= 0 {
										return layout.Dimensions{}
									}
									return v.layoutReplyCount(gtx, idx, msg)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if msg.Poll == nil {
										return layout.Dimensions{}
									}
									return v.layoutPoll(gtx, idx, msg)
								}),
							)
						})
					}),
				)
			}),
		)
		call := macro.Stop()

		// Highlight background (below content)
		if hovered && !isMentioned {
			paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 8}, clip.Rect{Max: dims.Size}.Op())
		}
		if isMentioned {
			paint.FillShape(gtx.Ops, color.NRGBA{R: 100, G: 80, B: 20, A: 30}, clip.Rect{Max: dims.Size}.Op())
		}

		// Replay content
		call.Add(gtx.Ops)

		// Action buttons overlay (top-right)
		if hovered {
			actMacro := op.Record(gtx.Ops)
			actDims := v.layoutActionBtns(gtx, idx, isOwn, msg.IsPinned, msg.IsHidden, msg.ID)
			actCall := actMacro.Stop()
			offX := dims.Size.X - actDims.Size.X
			if offX < 0 {
				offX = 0
			}
			st := op.Offset(image.Pt(offX, 0)).Push(gtx.Ops)
			actCall.Add(gtx.Ops)
			st.Pop()
		}

		// Pointer area for hover detection (on top with PassOp — Enter/Leave pass through,
		// clicks propagate down to content thanks to PassOp)
		pr := pointer.PassOp{}.Push(gtx.Ops)
		rect := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
		event.Op(gtx.Ops, &v.actions[idx].rowHovered)
		rect.Pop()
		pr.Pop()

		return dims
	})
}

func (v *MessageView) getEmojis() []api.CustomEmoji {
	conn := v.app.Conn()
	if conn == nil {
		return nil
	}
	return conn.Emojis
}

func (v *MessageView) layoutAttachments(gtx layout.Context, msgIdx int, msg api.Message) layout.Dimensions {
	conn := v.app.Conn()
	var items []layout.FlexChild
	for j, att := range msg.Attachments {
		if j >= len(v.actions[msgIdx].attBtns) {
			break
		}
		a := att
		attIdx := j
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				// Inline image preview for image/* MIME types
				if strings.HasPrefix(a.MimeType, "image/") && conn != nil {
					return v.layoutImagePreview(gtx, msgIdx, attIdx, a, conn.URL)
				}
				// Inline video preview for video/* MIME types
				if isVideoMIME(a.MimeType) && conn != nil {
					return v.layoutVideoPreview(gtx, msgIdx, attIdx, a, conn.URL)
				}
				// Fallback: clickable link
				btn := &v.actions[msgIdx].attBtns[attIdx]
				return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := ColorInput
					if btn.Hovered() {
						bg = ColorHover
					}
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(4)
							paint.FillShape(gtx.Ops, bg, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								sizeStr := FormatBytes(a.Size)
								lbl := material.Caption(v.app.Theme.Material, a.Filename+" ("+sizeStr+") — click to save")
								lbl.Color = ColorAccent
								return lbl.Layout(gtx)
							})
						},
					)
				})
			})
		}))
	}
	// Download All button for game server attachments
	if hasGameServerAttachments(msg.Attachments) {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				btn := &v.actions[msgIdx].downloadAllBtn
				return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := ColorAccent
					if btn.Hovered() {
						bg.A = 220
					}
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(4)
							paint.FillShape(gtx.Ops, bg, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layoutIcon(gtx, IconDownload, 14, ColorText)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(v.app.Theme.Material, fmt.Sprintf("Download All (%d files)", len(msg.Attachments)))
											lbl.Color = ColorText
											return lbl.Layout(gtx)
										})
									}),
								)
							})
						},
					)
				})
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}

// layoutLinkPreview renders a compact OpenGraph embed below the message.
func (v *MessageView) layoutLinkPreview(gtx layout.Context, msg api.Message, idx int) layout.Dimensions {
	lp := msg.LinkPreview
	if lp == nil || (lp.Title == "" && lp.Description == "") {
		return layout.Dimensions{}
	}

	btn := &v.actions[idx].linkPreviewBtn
	if btn.Clicked(gtx) {
		openURL(lp.URL)
	}

	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if btn.Hovered() {
				pointer.CursorPointer.Add(gtx.Ops)
			}
			// Limit preview width
			maxW := gtx.Dp(420)
			if gtx.Constraints.Max.X > maxW {
				gtx.Constraints.Max.X = maxW
			}

			bgColor := ColorInput
			accentBorder := ColorAccent

			return layout.Background{}.Layout(gtx,
				// Background with rounded corners + accent left border
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
					rr := gtx.Dp(6)
					paint.FillShape(gtx.Ops, bgColor, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
					// Left accent border (3px)
					borderW := gtx.Dp(3)
					paint.FillShape(gtx.Ops, accentBorder, clip.RRect{
						Rect: image.Rect(0, 0, borderW, bounds.Max.Y),
						NW:   rr, SW: rr,
					}.Op(gtx.Ops))
					return layout.Dimensions{Size: bounds.Max}
				},
				// Content
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// Site name
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if lp.SiteName == "" {
									return layout.Dimensions{}
								}
								lbl := material.Caption(v.app.Theme.Material, lp.SiteName)
								lbl.Color = ColorTextDim
								lbl.TextSize = v.app.Theme.Sp(11)
								return lbl.Layout(gtx)
							}),
							// Title
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if lp.Title == "" {
									return layout.Dimensions{}
								}
								return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Body2(v.app.Theme.Material, lp.Title)
									lbl.Color = ColorAccent
									lbl.Font.Weight = 600
									lbl.MaxLines = 2
									return lbl.Layout(gtx)
								})
							}),
							// Description
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if lp.Description == "" {
									return layout.Dimensions{}
								}
								return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, lp.Description)
									lbl.Color = ColorText
									lbl.MaxLines = 3
									return lbl.Layout(gtx)
								})
							}),
							// Thumbnail image
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if lp.ImageURL == "" {
									return layout.Dimensions{}
								}
								ci := v.app.Images.Get(lp.ImageURL, func() { v.app.Window.Invalidate() })
								if ci == nil || ci.img == nil {
									return layout.Dimensions{}
								}
								return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									imgW := ci.img.Bounds().Dx()
									imgH := ci.img.Bounds().Dy()
									if imgW == 0 || imgH == 0 {
										return layout.Dimensions{}
									}
									maxImgW := gtx.Constraints.Max.X
									maxImgH := gtx.Dp(160)
									scale := float32(maxImgW) / float32(imgW)
									if int(float32(imgH)*scale) > maxImgH {
										scale = float32(maxImgH) / float32(imgH)
									}
									if scale > 1 {
										scale = 1
									}
									w := int(float32(imgW) * scale)
									h := int(float32(imgH) * scale)
									rr := gtx.Dp(4)
									rc := clip.RRect{Rect: image.Rect(0, 0, w, h), NE: rr, NW: rr, SE: rr, SW: rr}
									stack := rc.Push(gtx.Ops)
									imgOp := paint.NewImageOp(ci.img)
									imgOp.Filter = paint.FilterLinear
									imgOp.Add(gtx.Ops)
									paint.PaintOp{}.Add(gtx.Ops)
									stack.Pop()
									return layout.Dimensions{Size: image.Pt(w, h)}
								})
							}),
						)
					})
				},
			)
		})
	})
}

func (v *MessageView) layoutImagePreview(gtx layout.Context, msgIdx, attIdx int, att api.Attachment, serverURL string) layout.Dimensions {
	url := serverURL + att.URL
	ci := v.app.Images.Get(url, func() { v.app.Window.Invalidate() })

	btn := &v.actions[msgIdx].attBtns[attIdx]

	if ci == nil {
		// Still loading
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Caption(v.app.Theme.Material, "Loading "+att.Filename+"...")
			lbl.Color = ColorTextDim
			return lbl.Layout(gtx)
		})
	}

	if !ci.ok {
		// Failed to load — show as regular attachment link
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, att.Filename+" ("+FormatBytes(att.Size)+") — click to save")
				lbl.Color = ColorAccent
				return lbl.Layout(gtx)
			})
		})
	}

	// Calculate display size (max 400x300, maintain aspect ratio)
	imgBounds := ci.img.Bounds()
	imgW := imgBounds.Dx()
	imgH := imgBounds.Dy()
	maxW := gtx.Dp(400)
	maxH := gtx.Dp(300)

	if imgW > maxW {
		imgH = imgH * maxW / imgW
		imgW = maxW
	}
	if imgH > maxH {
		imgW = imgW * maxH / imgH
		imgH = maxH
	}

	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// Draw the image scaled to display size
				origW := float32(ci.img.Bounds().Dx())
				origH := float32(ci.img.Bounds().Dy())
				scaleX := float32(imgW) / origW
				scaleY := float32(imgH) / origH

				// Clip with rounded corners
				rr := gtx.Dp(6)
				defer clip.RRect{
					Rect: image.Rect(0, 0, imgW, imgH),
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Push(gtx.Ops).Pop()

				defer op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(scaleX, scaleY))).Push(gtx.Ops).Pop()
				ci.op.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)

				return layout.Dimensions{Size: image.Pt(imgW, imgH)}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(v.app.Theme.Material, att.Filename+" ("+FormatBytes(att.Size)+")")
				lbl.Color = ColorTextDim
				return lbl.Layout(gtx)
			}),
		)
	})
}

func (v *MessageView) layoutActionBtns(gtx layout.Context, idx int, isOwn bool, isPinned, isHidden bool, msgID string) layout.Dimensions {
	var perms int64
	if conn := v.app.Conn(); conn != nil {
		perms = conn.MyPermissions
	}
	canManage := perms&(api.PermManageMessages|api.PermAdmin) != 0
	canPin := perms&(api.PermManageMessages|api.PermAdmin) != 0

	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
			rr := gtx.Dp(6)
			paint.FillShape(gtx.Ops, ColorCard, clip.RRect{
				Rect: bounds,
				NE:   rr, NW: rr, SE: rr, SW: rr,
			}.Op(gtx.Ops))
			return layout.Dimensions{Size: bounds.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutSmallAction(gtx, &v.actions[idx].thumbUpBtn, "\U0001f44d", ColorTextDim)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallAction(gtx, &v.actions[idx].thumbDownBtn, "\U0001f44e", ColorTextDim)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallIconAction(gtx, &v.actions[idx].reactEmojiBtn, IconAdd, ColorAccent)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallIconAction(gtx, &v.actions[idx].copyBtn, IconCopy, ColorTextDim)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						icon := IconBookmarkBorder
						clr := ColorTextDim
						if v.app.Bookmarks != nil {
							if conn := v.app.Conn(); conn != nil && v.app.Bookmarks.IsBookmarked(msgID, conn.URL) {
								icon = IconBookmark
								clr = ColorAccent
							}
						}
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallIconAction(gtx, &v.actions[idx].bookmarkBtn, icon, clr)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallIconAction(gtx, &v.actions[idx].replyBtn, IconReply, ColorTextDim)
						})
					}),
					// Pin — only with ManageChannels or Admin
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !canPin {
							return layout.Dimensions{}
						}
						pinLabel := "Pin"
						if isPinned {
							pinLabel = "Unpin"
						}
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallAction(gtx, &v.actions[idx].pinBtn, pinLabel, ColorAccent)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if isOwn {
							return layout.Dimensions{}
						}
						hideLabel := "Hide"
						if isHidden {
							hideLabel = "Show"
						}
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallAction(gtx, &v.actions[idx].hideBtn, hideLabel, ColorTextDim)
						})
					}),
					// Edit — own messages only
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !isOwn {
							return layout.Dimensions{}
						}
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallIconAction(gtx, &v.actions[idx].editBtn, IconEdit, ColorTextDim)
						})
					}),
					// Delete — author or ManageMessages/Admin
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !isOwn && !canManage {
							return layout.Dimensions{}
						}
						return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutSmallIconAction(gtx, &v.actions[idx].deleteBtn, IconDelete, ColorDanger)
						})
					}),
				)
			})
		},
	)
}

func (v *MessageView) layoutSmallAction(gtx layout.Context, btn *widget.Clickable, text string, clr color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if btn.Hovered() {
			bg = ColorHover
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				if bg.A > 0 {
					rr := gtx.Dp(3)
					paint.FillShape(gtx.Ops, bg, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
				}
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					// Try Twemoji image for emoji text
					if len(text) > 0 {
						runes := []rune(text)
						if len(runes) <= 2 && isEmojiRune(runes[0]) {
							if dims, ok := layoutTwemoji(gtx, v.app, text, 20); ok {
								return dims
							}
						}
					}
					lbl := material.Body2(v.app.Theme.Material, text)
					lbl.Color = clr
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (v *MessageView) layoutReplyRef(gtx layout.Context, reply *api.Message) layout.Dimensions {
	author := v.app.ResolveUserName(reply.Author)
	content := reply.Content
	if len(content) > 80 {
		content = content[:80] + "..."
	}

	return layout.Inset{Left: unit.Dp(46), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.End}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// Vertical bar
				size := image.Pt(gtx.Dp(2), gtx.Dp(14))
				paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Max: size}.Op())
				return layout.Dimensions{Size: size}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(v.app.Theme.Material, author+": "+content)
					lbl.Color = ColorTextDim
					return lbl.Layout(gtx)
				})
			}),
		)
	})
}

func (v *MessageView) layoutReplyPreview(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.End}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							size := image.Pt(gtx.Dp(2), gtx.Dp(14))
							paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Max: size}.Op())
							return layout.Dimensions{Size: size}
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								preview := v.ReplyToContent
								if len(preview) > 60 {
									preview = preview[:60] + "..."
								}
								lbl := material.Caption(v.app.Theme.Material, "Replying to "+v.ReplyToAuthor+": "+preview)
								lbl.Color = ColorTextDim
								return lbl.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.cancelReply.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconClose, 16, ColorDanger)
								})
							})
						}),
					)
				})
			},
		)
	})
}

func (v *MessageView) layoutEditIndicator(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(6)
				paint.FillShape(gtx.Ops, ColorSidebar, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.End}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(v.app.Theme.Material, "Editing message — press Enter to save, Escape to cancel")
							lbl.Color = ColorAccent
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.cancelEdit.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconClose, 16, ColorDanger)
								})
							})
						}),
					)
				})
			},
		)
	})
}

func (v *MessageView) layoutTypingIndicator(gtx layout.Context) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil {
		return layout.Dimensions{}
	}

	v.app.mu.RLock()
	chID := conn.ActiveChannelID
	typers := conn.ChannelTyping[chID]
	v.app.mu.RUnlock()

	if len(typers) == 0 {
		return layout.Dimensions{}
	}

	// Build typing text — filter out old (>5s) and own user
	names := ""
	count := 0
	now := time.Now()
	for uid, t := range typers {
		if now.Sub(t) > 5*time.Second {
			continue
		}
		if uid == conn.UserID {
			continue
		}
		name := "?"
		for _, u := range conn.Users {
			if u.ID == uid {
				name = v.app.ResolveUserName(&u)
				break
			}
		}
		if count > 0 {
			names += ", "
		}
		names += name
		count++
	}

	if count == 0 {
		return layout.Dimensions{}
	}

	// Schedule redraw after 5s to clear expired typing indicators
	gtx.Execute(op.InvalidateCmd{At: time.Now().Add(5 * time.Second)})

	text := names + " is typing..."
	if count > 1 {
		text = names + " are typing..."
	}

	return layout.Inset{Left: unit.Dp(16), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Caption(v.app.Theme.Material, text)
		lbl.Color = ColorTextDim
		return lbl.Layout(gtx)
	})
}

func (v *MessageView) sendMessage() {
	text := v.editor.Text()

	// Collect results of completed uploads
	v.uploadMu.Lock()
	var results []api.UploadResult
	for _, u := range v.pendingUploads {
		if u.status == 1 && u.result != nil && !u.removed {
			results = append(results, *u.result)
		}
	}
	savedChID := v.uploadChannelID
	v.pendingUploads = nil
	v.uploadChannelID = ""
	v.uploadMu.Unlock()

	// Close upload popup
	v.app.UploadDlg.Hide()

	if text == "" && len(results) == 0 {
		return
	}
	v.editor.SetText("")

	replyTo := v.ReplyToID
	v.ReplyToID = ""
	v.ReplyToContent = ""
	v.ReplyToAuthor = ""

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	go func() {
		// If we have a captured channelID from upload (auto-send), use that
		chID := savedChID
		if chID == "" {
			v.app.mu.RLock()
			chID = conn.ActiveChannelID
			v.app.mu.RUnlock()
		}

		if chID == "" {
			return
		}
		var sendErr error
		if len(results) > 0 {
			_, sendErr = conn.Client.SendMessageWithAttachments(chID, text, replyTo, results)
		} else {
			_, sendErr = conn.Client.SendMessage(chID, text, replyTo)
		}
		if sendErr != nil {
			errStr := sendErr.Error()
			if strings.Contains(errStr, "slow mode") {
				v.slowModeErr = errStr
				v.slowModeUntil = time.Now().Add(5 * time.Second)
				v.app.Window.Invalidate()
			} else {
				log.Printf("SendMessage error: %v", sendErr)
				v.app.Toasts.Error("Failed to send message")
			}
		}
	}()
}

func (v *MessageView) submitEdit() {
	text := v.editor.Text()
	if text == "" {
		return
	}
	v.editor.SetText("")
	msgID := v.EditingID
	v.EditingID = ""

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	go func() {
		if err := conn.Client.EditMessage(msgID, text); err != nil {
			log.Printf("EditMessage error: %v", err)
			v.app.Toasts.Error("Failed to edit message")
		}
	}()
}

func (v *MessageView) sendTyping() {
	now := time.Now()
	if now.Sub(v.lastTypingSent) < 3*time.Second {
		return
	}
	v.lastTypingSent = now

	conn := v.app.Conn()
	if conn == nil {
		return
	}

	go func() {
		v.app.mu.RLock()
		chID := conn.ActiveChannelID
		v.app.mu.RUnlock()

		payload, _ := json.Marshal(map[string]string{
			"channel_id": chID,
		})
		conn.WS.Send(api.WSEvent{
			Type:    "channel.typing",
			Payload: payload,
		})
	}()
}

func (v *MessageView) loadOlderMessages() {
	v.loadingOlder = true
	conn := v.app.Conn()
	if conn == nil {
		v.loadingOlder = false
		return
	}

	go func() {
		defer func() { v.loadingOlder = false }()

		v.app.mu.RLock()
		chID := conn.ActiveChannelID
		msgs := conn.Messages
		v.app.mu.RUnlock()

		if chID == "" || len(msgs) == 0 {
			return
		}

		before := msgs[0].ID
		older, err := conn.Client.GetMessages(chID, before, 50)
		if err != nil {
			log.Printf("LoadOlder error: %v", err)
			return
		}

		if len(older) == 0 {
			v.noMoreOlder = true
			return
		}

		// Reverse (server returns newest first)
		for i, j := 0, len(older)-1; i < j; i, j = i+1, j-1 {
			older[i], older[j] = older[j], older[i]
		}

		v.app.mu.Lock()
		conn.Messages = append(older, conn.Messages...)
		// Adjust scroll position to keep current view
		v.list.Position.First += len(older)
		v.app.mu.Unlock()
		v.app.Window.Invalidate()
	}()
}

func (v *MessageView) layoutCircleIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon, disabled bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Dp(36)
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min
		bg := ColorInput
		if btn.Hovered() && !disabled {
			bg = ColorHover
		}
		if disabled {
			bg = ColorTextDim
		}
		rr := size / 2
		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			fg := ColorText
			if disabled {
				fg = ColorTextDim
			}
			return layoutIcon(gtx, icon, 20, fg)
		})
	})
}

func (v *MessageView) layoutSmallIconAction(gtx layout.Context, btn *widget.Clickable, icon *NIcon, clr color.NRGBA) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		if btn.Hovered() {
			bg = ColorHover
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				if bg.A > 0 {
					rr := gtx.Dp(3)
					paint.FillShape(gtx.Ops, bg, clip.RRect{
						Rect: bounds,
						NE:   rr, NW: rr, SE: rr, SW: rr,
					}.Op(gtx.Ops))
				}
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutIcon(gtx, icon, 16, clr)
				})
			},
		)
	})
}

func (v *MessageView) layoutCircleBtn(gtx layout.Context, btn *widget.Clickable, text string, disabled bool) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Dp(36)
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min
		bg := ColorInput
		if btn.Hovered() && !disabled {
			bg = ColorHover
		}
		if disabled {
			bg = ColorTextDim
		}
		rr := size / 2
		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(v.app.Theme.Material, text)
			lbl.Color = ColorText
			if disabled {
				lbl.Color = ColorTextDim
			}
			return lbl.Layout(gtx)
		})
	})
}
func (v *MessageView) ResetScrollState() {
	v.noMoreOlder = false
	v.loadingOlder = false
	v.ReplyToID = ""
	v.EditingID = ""
	v.editor.SetText("")
	// Uploads are not cancelled — they run in background with uploadChannelID
	v.showMentions = false
	v.showPins = false
	v.pinExpanded = false
	v.pinnedMsgs = nil
	v.pinsListLinks = nil
	v.pinsListSels = nil
	v.pinsListActs = nil
	v.showSearch = false
	v.searchResults = nil
	v.searchActions = nil
	v.searchQuery = ""
	v.searchOffset = 0
	v.searchEditor.SetText("")
	v.p2pBarBtns = make(map[string]*widget.Clickable)
}

// layoutReplyCount renders a clickable "N replies" label that opens the thread view.
func (v *MessageView) layoutReplyCount(gtx layout.Context, idx int, msg api.Message) layout.Dimensions {
	if v.actions[idx].replyCountBtn.Clicked(gtx) {
		v.app.ThreadView.Open(msg.ID)
	}
	return v.actions[idx].replyCountBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		label := fmt.Sprintf("%d replies", msg.ReplyCount)
		if msg.ReplyCount == 1 {
			label = "1 reply"
		}
		lbl := material.Caption(v.app.Theme.Material, label)
		lbl.Color = ColorAccent
		if v.actions[idx].replyCountBtn.Hovered() {
			lbl.Color = ColorText
		}
		return lbl.Layout(gtx)
	})
}
