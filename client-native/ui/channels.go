package ui

import (
	"image"
	"image/color"
	"log"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"nora-client/api"
	"nora-client/store"
)

type chanCatChild struct {
	id        string
	name      string
	hexColor  string
	channels  []int
	widgetIdx int // index into catHeaderBtns etc.
}

type chanCatGroup struct {
	name     string
	hexColor string
	channels []int
	children []chanCatChild
}

type ChannelView struct {
	app          *App
	list         widget.List
	chanBtns     []widget.Clickable
	chanDelBtns  []widget.Clickable
	chanEditBtns []widget.Clickable
	chanDrags    []gesture.Drag
	chanRightTags []bool // pointer event tags for right-click on channel
	streamBtns      map[string]*widget.Clickable // per-user stream watch button
	liveWBBtns      map[string]*widget.Clickable // per-user live WB button (pen icon)
	voiceUserBtns   map[string]*widget.Clickable // per-user click → UserPopup
	subChannelBtns  map[string]*widget.Clickable // per sub-channel click → voice join
	createBtn      widget.Clickable
	libraryBtn     widget.Clickable
	whiteboardBtn  widget.Clickable
	gameServersBtn widget.Clickable
	settingsBtn    widget.Clickable // Server settings gear in header
	markReadBtn    widget.Clickable // Mark all as read

	// Channel drag state
	dragging      bool
	dragFromIdx   int    // index in flat channel list
	dragFromID    string // channel ID being dragged
	dragStartY    float32
	dragOffsetY   float32 // current Y delta from start
	dragTargetIdx int     // where we'd drop (-1 = none, negative sentinel = empty cat)
	dragStepPx    int     // pixel size of one step (dp-aware)

	// Visual order: flat channel indices as they appear on screen (category-grouped)
	// Negative values = empty category sentinel: -(emptyCatPos+1)
	visualOrder    []int
	emptyCatIDs    []string // category IDs for sentinels

	// Root category channel slot: channelID → child index after which channel is displayed
	// -1 = before first child, 0 = after child[0], 1 = after child[1], ...
	rootChanSlot map[string]int
	// Root gap sentinels: emptyCatIDs index → slot position (for drop line rendering)
	rootGapSentinels map[int]int

	// Per-channel rendered heights (flat index → pixel height)
	chanItemH map[int]int

	// Lobby sub-channel mapping (rebuilt every frame)
	lobbyChildren map[string][]int // lobbyID → indices of child channels

	// Position of uncategorized channels among categories (channelID → index in renderedCatIDs, -1 = before first)
	uncatPositions map[string]int

	// Category collapse state (in-memory only, resets on restart)
	collapsedCats map[string]bool

	// Category header actions
	catHeaderBtns []widget.Clickable // collapse toggle
	catEditBtns2  []widget.Clickable // enter edit mode (in header)
	catDelBtns2   []widget.Clickable // delete category (in header)

	// Voice user drag-and-drop (moving user between voice channels)
	voiceDrags          map[string]*gesture.Drag
	voiceDragging       bool
	voiceDragUserID     string   // ID of user being moved
	voiceDragFromChanID string   // source voice channel
	voiceDragStartY     float32
	voiceDragOffsetY    float32
	voiceDragTargetChan string   // target voice channel ID ("" = none)
	voiceChanIDs        []string // voice channels in display order (rebuilt every frame)

	// Category drag-and-drop reorder + reparent
	catDrags          []gesture.Drag
	catDragging       bool
	catDragFromIdx    int    // index in renderedCatIDs (top-level), or -1 for child
	catDragFromID     string // category ID
	catDragFromParent string // parent ID (empty = top-level)
	catDragStartY     float32 // ev.Position.Y on Press
	catDragOffsetY    float32 // cursor delta from start
	catDragTargetIdx  int    // target index in renderedCatIDs (-1 = none)
	catDragTargetID   string // target category ID for adopt ("" = reorder/unadopt)
	catDragAction     string // "reorder" | "adopt" | "child_reorder" | "" (none)
	catDragChildFrom  int    // source index among siblings
	catDragChildTo    int    // target index among siblings
	catDragSiblings   []string // sibling IDs in order
	renderedCatIDs    []string // saved for D&D
	catHeights        []int    // height of each category in px
	allCatInfos       []catDragInfo // flat list of all categories for D&D
	renderItemHeights []int // height of each render item (categories + uncategorized blocks)
	catRenderIdx      []int // renderedCatIDs[i] → index in renderItems

}

// catDragInfo — info about a category for D&D targeting
type catDragInfo struct {
	id         string
	parentID   string // "" = top-level
	hasChildren bool
	dragIdx    int    // index in catDrags
}

func NewChannelView(a *App) *ChannelView {
	v := &ChannelView{app: a}
	v.list.Axis = layout.Vertical
	v.dragTargetIdx = -1
	v.dragFromIdx = -1
	v.collapsedCats = make(map[string]bool)
	v.uncatPositions = make(map[string]int)
	v.catDragTargetIdx = -1
	v.catDragFromIdx = -1
	v.streamBtns = make(map[string]*widget.Clickable)
	v.liveWBBtns = make(map[string]*widget.Clickable)
	v.voiceUserBtns = make(map[string]*widget.Clickable)
	v.subChannelBtns = make(map[string]*widget.Clickable)
	v.voiceDrags = make(map[string]*gesture.Drag)
	v.chanItemH = make(map[int]int)
	return v
}

func (v *ChannelView) Layout(gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, ColorCard, clip.Rect{Max: gtx.Constraints.Max}.Op())



	conn := v.app.Conn()
	if conn == nil {
		return layoutColoredBg(gtx, ColorCard)
	}

	if v.app.LANHelper != nil {
		v.app.LANHelper.CheckHelper()
	}

	v.app.mu.RLock()
	channels := conn.Channels
	categories := conn.Categories
	activeID := conn.ActiveChannelID
	serverName := conn.Name
	serverDesc := conn.Description
	v.app.mu.RUnlock()

	// Mark all as read
	if v.markReadBtn.Clicked(gtx) {
		v.app.mu.Lock()
		for k := range conn.UnreadCount {
			delete(conn.UnreadCount, k)
		}
		for k := range conn.UnreadDMCount {
			delete(conn.UnreadDMCount, k)
		}
		for k := range conn.UnreadGroups {
			delete(conn.UnreadGroups, k)
		}
		v.app.mu.Unlock()
	}

	if len(v.chanBtns) < len(channels) {
		v.chanBtns = make([]widget.Clickable, len(channels)+10)
	}
	if len(v.chanDelBtns) < len(channels) {
		v.chanDelBtns = make([]widget.Clickable, len(channels)+10)
	}
	if len(v.chanEditBtns) < len(channels) {
		v.chanEditBtns = make([]widget.Clickable, len(channels)+10)
	}
	if len(v.chanDrags) < len(channels) {
		v.chanDrags = make([]gesture.Drag, len(channels)+10)
	}
	if len(v.chanRightTags) < len(channels) {
		v.chanRightTags = make([]bool, len(channels)+10)
	}

	// Allocate category header widgets (top-level + children)
	catCount := len(categories)
	for _, cat := range categories {
		catCount += len(cat.Children)
	}
	if len(v.catHeaderBtns) < catCount {
		v.catHeaderBtns = make([]widget.Clickable, catCount+5)
	}
	if len(v.catEditBtns2) < catCount {
		v.catEditBtns2 = make([]widget.Clickable, catCount+5)
	}
	if len(v.catDelBtns2) < catCount {
		v.catDelBtns2 = make([]widget.Clickable, catCount+5)
	}
	if len(v.catDrags) < catCount {
		v.catDrags = make([]gesture.Drag, catCount+5)
	}
	if len(v.catHeights) < catCount {
		v.catHeights = make([]int, catCount+5)
	}

	// Process ALL drag events BEFORE list rendering.
	// This prevents the scroll gesture from grabbing the pointer first.
	v.dragStepPx = gtx.Dp(32)
	if v.dragStepPx < 16 {
		v.dragStepPx = 16
	}
	for i := 0; i < len(channels) && i < len(v.chanDrags); i++ {
		for {
			ev, ok := v.chanDrags[i].Update(gtx.Metric, gtx.Source, gesture.Vertical)
			if !ok {
				break
			}
			switch ev.Kind {
			case pointer.Press:
				v.dragging = true
				v.dragFromIdx = i
				if i < len(channels) {
					v.dragFromID = channels[i].ID
				}
				v.dragStartY = ev.Position.Y
				v.dragOffsetY = 0
				v.dragTargetIdx = i
			case pointer.Drag:
				if v.dragging && i == v.dragFromIdx {
					v.dragOffsetY = ev.Position.Y - v.dragStartY
					v.computeDragTarget()
				}
			case pointer.Release, pointer.Cancel:
				if v.dragging && i == v.dragFromIdx {
					v.executeDrop()
				}
			}
		}
	}

	// Process right-click events on channels
	for i := 0; i < len(channels) && i < len(v.chanRightTags); i++ {
		ch := channels[i]
		if ch.Type != "" && ch.Type != "text" {
			continue
		}
		for {
			ev, ok := gtx.Event(pointer.Filter{
				Target: &v.chanRightTags[i],
				Kinds:  pointer.Press,
			})
			if !ok {
				break
			}
			if pe, ok := ev.(pointer.Event); ok && pe.Buttons == pointer.ButtonSecondary {
				_ = pe
				v.showChannelNotifyMenu(ch.ID, v.app.CursorX, v.app.CursorY)
			}
		}
	}

	// Process category drag events (all — top-level and children)
	for i := 0; i < catCount && i < len(v.catDrags); i++ {
		for {
			ev, ok := v.catDrags[i].Update(gtx.Metric, gtx.Source, gesture.Vertical)
			if !ok {
				break
			}
			switch ev.Kind {
			case pointer.Press:
				v.catDragging = true
				v.catDragFromIdx = -1
				v.catDragFromID = ""
				v.catDragFromParent = ""
				// Find category info by drag index
				for _, ci := range v.allCatInfos {
					if ci.dragIdx == i {
						v.catDragFromID = ci.id
						v.catDragFromParent = ci.parentID
						// Find index in renderedCatIDs (only for top-level)
						if ci.parentID == "" {
							for ri, rid := range v.renderedCatIDs {
								if rid == ci.id {
									v.catDragFromIdx = ri
									break
								}
							}
						}
						break
					}
				}
				v.catDragStartY = ev.Position.Y
				v.catDragOffsetY = 0
				v.catDragTargetIdx = v.catDragFromIdx
				v.catDragAction = ""
			case pointer.Drag:
				if v.catDragging && v.catDragFromID != "" {
					v.catDragOffsetY = ev.Position.Y - v.catDragStartY
					v.computeCatDragTarget()
				}
			case pointer.Release, pointer.Cancel:
				if v.catDragging && v.catDragFromID != "" {
					v.executeCatDrop()
				}
			}
		}
	}

	// Process voice user drag events (moving between voice channels)
	for uid, drag := range v.voiceDrags {
		for {
			ev, ok := drag.Update(gtx.Metric, gtx.Source, gesture.Vertical)
			if !ok {
				break
			}
			switch ev.Kind {
			case pointer.Press:
				v.voiceDragging = true
				v.voiceDragUserID = uid
				v.voiceDragStartY = ev.Position.Y
				v.voiceDragOffsetY = 0
				v.voiceDragTargetChan = ""
				// Find source voice channel
				v.voiceDragFromChanID = ""
				if c := v.app.Conn(); c != nil {
					v.app.mu.RLock()
					for chID, users := range c.VoiceState {
						for _, u := range users {
							if u == uid {
								v.voiceDragFromChanID = chID
								break
							}
						}
						if v.voiceDragFromChanID != "" {
							break
						}
					}
					v.app.mu.RUnlock()
				}
			case pointer.Drag:
				if v.voiceDragging && uid == v.voiceDragUserID {
					v.voiceDragOffsetY = ev.Position.Y - v.voiceDragStartY
					v.computeVoiceDragTarget()
				}
			case pointer.Release, pointer.Cancel:
				if v.voiceDragging && uid == v.voiceDragUserID {
					v.executeVoiceDrop()
				}
			}
		}
	}

	// Handle lobby join dialog result
	if lobbyID, name, pw, ok := v.app.LobbyJoinDlg.HandleResult(); ok {
		if c := v.app.Conn(); c != nil && c.Voice != nil {
			if c.Call != nil && c.Call.IsActive() {
				c.Call.HangupCall()
			}
			c.Voice.JoinWithOptions(lobbyID, name, pw)
			// Save to lobby cache
			go store.UpdateLobbyPrefs(v.app.PublicKey, c.URL, lobbyID, store.LobbyPrefs{
				LastName:     name,
				LastPassword: pw,
			})
		}
	}

	// Handle create button
	if v.createBtn.Clicked(gtx) {
		v.app.CreateDlg.Show()
	}
	if v.libraryBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewLibrary
		v.app.mu.Unlock()
		v.app.Library.Open()
	}
	// Whiteboard removed from channels — use voice channel whiteboard instead
	if v.gameServersBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewGameServers
		v.app.mu.Unlock()
	}
	if v.settingsBtn.Clicked(gtx) {
		v.app.mu.Lock()
		v.app.Mode = ViewSettings
		v.app.mu.Unlock()
		v.app.Settings.activeCategory = settingsCatServer
	}

	// Handle category header clicks (collapse, edit, delete, save, cancel)
	// Clicked() calls Update() → updates hover state. Therefore process clicks first.
	// Iterate over ALL categories (top-level + children) with unique widget indices
	type catInfo struct {
		id    string
		name  string
		color string
	}
	var allCats []catInfo
	for _, cat := range categories {
		allCats = append(allCats, catInfo{id: cat.ID, name: cat.Name, color: cat.Color})
		for _, child := range cat.Children {
			allCats = append(allCats, catInfo{id: child.ID, name: child.Name, color: child.Color})
		}
	}
	for i, cat := range allCats {
		if i >= len(v.catHeaderBtns) {
			break
		}

		// Process events (updates hover state)
		headerClicked := v.catHeaderBtns[i].Clicked(gtx)
		editClicked := v.catEditBtns2[i].Clicked(gtx)
		deleteClicked := v.catDelBtns2[i].Clicked(gtx)

		// Hover state (now current)
		catHovered := v.catHeaderBtns[i].Hovered() ||
			v.catEditBtns2[i].Hovered() || v.catDelBtns2[i].Hovered()

		// Collapse toggle (skip if edit or delete was clicked)
		if headerClicked && !editClicked && !deleteClicked {
			v.collapsedCats[cat.id] = !v.collapsedCats[cat.id]
		}

		// Edit button — open popup dialog
		if editClicked && catHovered {
			v.app.CatEditDlg.Show(cat.id, cat.name, cat.color)
		}

		// Delete button (only on hover and if category has no channels)
		if deleteClicked && catHovered {
			catID := cat.id
			hasChannels := false
			for _, ch := range channels {
				if ch.CategoryID != nil && *ch.CategoryID == catID {
					hasChannels = true
					break
				}
			}
			if !hasChannels {
				go func() {
					if c := v.app.Conn(); c != nil {
						if err := c.Client.DeleteCategory(catID); err != nil {
							log.Printf("DeleteCategory: %v", err)
							return
						}
						// Full refresh via WS event
						v.app.Window.Invalidate()
					}
				}()
			}
		}
	}

	catMap := make(map[string]*chanCatGroup)
	childCatMap := make(map[string]*chanCatChild) // child catID → chanCatChild (for channel assignment)
	var uncategorized []int
	var orderedCats []string

	for _, cat := range categories {
		g := &chanCatGroup{name: cat.Name, hexColor: cat.Color}
		for _, ch := range cat.Children {
			child := chanCatChild{id: ch.ID, name: ch.Name, hexColor: ch.Color}
			g.children = append(g.children, child)
		}
		catMap[cat.ID] = g
		orderedCats = append(orderedCats, cat.ID)
		// Save child reference
		for ci := range g.children {
			childCatMap[g.children[ci].id] = &g.children[ci]
		}
	}

	// Group sub-channels (ParentID != nil) under parent lobby channel
	v.lobbyChildren = make(map[string][]int) // lobbyID → []channel indices
	for i, ch := range channels {
		if ch.ParentID != nil && *ch.ParentID != "" {
			v.lobbyChildren[*ch.ParentID] = append(v.lobbyChildren[*ch.ParentID], i)
			continue // sub-channels are not in the main list
		}
		if ch.CategoryID != nil && *ch.CategoryID != "" {
			// Try top-level
			if g, ok := catMap[*ch.CategoryID]; ok {
				g.channels = append(g.channels, i)
				continue
			}
			// Try child
			if child, ok := childCatMap[*ch.CategoryID]; ok {
				child.channels = append(child.channels, i)
				continue
			}
		}
		uncategorized = append(uncategorized, i)
	}

	// Mapping cat ID → widget index (unique for both top-level and children)
	catIdxMap := make(map[string]int, catCount)
	{
		idx := 0
		for _, cat := range categories {
			catIdxMap[cat.ID] = idx
			idx++
			for _, child := range cat.Children {
				catIdxMap[child.ID] = idx
				idx++
			}
		}
		// Set widgetIdx on chanCatChild in catMap
		for _, g := range catMap {
			for ci := range g.children {
				if wi, ok := catIdxMap[g.children[ci].id]; ok {
					g.children[ci].widgetIdx = wi
				}
			}
		}
	}

	renderedCatIDs := make([]string, 0, len(orderedCats))
	for _, catID := range orderedCats {
		if catMap[catID] != nil {
			renderedCatIDs = append(renderedCatIDs, catID)
		}
	}

	// Split uncategorized channels into slots between categories
	// uncatSlots[-1] = before first category, uncatSlots[i] = after renderedCatIDs[i]
	uncatSlots := make(map[int][]int)
	for _, chIdx := range uncategorized {
		chID := channels[chIdx].ID
		if pos, ok := v.uncatPositions[chID]; ok && pos >= -1 && pos < len(renderedCatIDs) {
			// User explicitly placed channel at this position (gap drop)
			uncatSlots[pos] = append(uncatSlots[pos], chIdx)
		} else {
			// Default: after last category
			slot := len(renderedCatIDs) - 1
			if len(renderedCatIDs) == 0 {
				slot = -1
			}
			uncatSlots[slot] = append(uncatSlots[slot], chIdx)
		}
	}

	// Build renderItems: interleaving categories and uncategorized blocks
	type chanRenderItem struct {
		isCat  bool
		catID  string
		catIdx int   // index v renderedCatIDs
		chans  []int // for uncategorized blocks
	}
	var renderItems []chanRenderItem
	v.catRenderIdx = v.catRenderIdx[:0]
	if chans := uncatSlots[-1]; len(chans) > 0 {
		renderItems = append(renderItems, chanRenderItem{chans: chans})
	}
	for ci, catID := range renderedCatIDs {
		v.catRenderIdx = append(v.catRenderIdx, len(renderItems))
		renderItems = append(renderItems, chanRenderItem{isCat: true, catID: catID, catIdx: ci})
		if chans := uncatSlots[ci]; len(chans) > 0 {
			renderItems = append(renderItems, chanRenderItem{chans: chans})
		}
	}
	groupCount := len(renderItems)


	// Build visual order: channel indices as they appear on screen
	// Negative sentinel -(pos+1): emptyCatIDs[pos] = catID (empty cat) or "" (gap between cats)
	v.visualOrder = v.visualOrder[:0]
	v.emptyCatIDs = v.emptyCatIDs[:0]
	if v.rootChanSlot == nil {
		v.rootChanSlot = make(map[string]int)
	}
	if v.rootGapSentinels == nil {
		v.rootGapSentinels = make(map[int]int)
	}
	for k := range v.rootGapSentinels {
		delete(v.rootGapSentinels, k)
	}
	// Gap sentinel before first category (for drop above)
	if len(renderedCatIDs) > 0 {
		sentinel := -(len(v.emptyCatIDs) + 1)
		v.emptyCatIDs = append(v.emptyCatIDs, "") // "" = gap
		v.visualOrder = append(v.visualOrder, sentinel)
	}
	// Uncategorized before first category
	if chans := uncatSlots[-1]; len(chans) > 0 {
		v.visualOrder = append(v.visualOrder, chans...)
	}
	for ci, catID := range renderedCatIDs {
		g := catMap[catID]
		collapsed := v.collapsedCats[catID]
		if g != nil && len(g.children) > 0 {
			if !collapsed {
				// Group root channels by slot position among children
				slotChans := make(map[int][]int) // slot → channel indices
				for _, ci := range g.channels {
					slot := -1
					if s, ok := v.rootChanSlot[channels[ci].ID]; ok {
						slot = s
						// Clamp to valid range
						if slot >= len(g.children) {
							slot = len(g.children) - 1
						}
					}
					slotChans[slot] = append(slotChans[slot], ci)
				}

				// Channels before first child (slot -1)
				if chans := slotChans[-1]; len(chans) > 0 {
					v.visualOrder = append(v.visualOrder, chans...)
				}
				// Sentinel before first child
				{
					sentinel := -(len(v.emptyCatIDs) + 1)
					v.emptyCatIDs = append(v.emptyCatIDs, catID)
					v.rootGapSentinels[len(v.emptyCatIDs)-1] = -1
					v.visualOrder = append(v.visualOrder, sentinel)
				}

				for childIdx, child := range g.children {
					childCollapsed := v.collapsedCats[child.id]
					if len(child.channels) > 0 && !childCollapsed {
						v.visualOrder = append(v.visualOrder, child.channels...)
					} else if !childCollapsed {
						sentinel := -(len(v.emptyCatIDs) + 1)
						v.emptyCatIDs = append(v.emptyCatIDs, child.id)
						v.visualOrder = append(v.visualOrder, sentinel)
					}
					// Channels after this child (slot childIdx)
					if chans := slotChans[childIdx]; len(chans) > 0 {
						v.visualOrder = append(v.visualOrder, chans...)
					}
					// Sentinel after this child
					sentinel := -(len(v.emptyCatIDs) + 1)
					v.emptyCatIDs = append(v.emptyCatIDs, catID)
					v.rootGapSentinels[len(v.emptyCatIDs)-1] = childIdx
					v.visualOrder = append(v.visualOrder, sentinel)
				}
			}
		} else if g != nil && len(g.channels) > 0 && !collapsed {
			v.visualOrder = append(v.visualOrder, g.channels...)
		} else if !collapsed {
			sentinel := -(len(v.emptyCatIDs) + 1)
			v.emptyCatIDs = append(v.emptyCatIDs, catID)
			v.visualOrder = append(v.visualOrder, sentinel)
		}
		// Gap sentinel after category
		sentinel := -(len(v.emptyCatIDs) + 1)
		v.emptyCatIDs = append(v.emptyCatIDs, "") // "" = gap
		v.visualOrder = append(v.visualOrder, sentinel)
		// Uncategorized channels after this category
		if chans := uncatSlots[ci]; len(chans) > 0 {
			v.visualOrder = append(v.visualOrder, chans...)
		}
	}

	// Save renderedCatIDs for category D&D
	v.renderedCatIDs = renderedCatIDs

	// Build flat list of info about all categories (for cat D&D targeting)
	v.allCatInfos = v.allCatInfos[:0]
	for _, cat := range categories {
		di := catIdxMap[cat.ID]
		v.allCatInfos = append(v.allCatInfos, catDragInfo{
			id:          cat.ID,
			parentID:    "",
			hasChildren: len(cat.Children) > 0,
			dragIdx:     di,
		})
		for _, child := range cat.Children {
			cdi := catIdxMap[child.ID]
			v.allCatInfos = append(v.allCatInfos, catDragInfo{
				id:          child.ID,
				parentID:    cat.ID,
				hasChildren: false,
				dragIdx:     cdi,
			})
		}
	}

	// Build list of voice channels in display order (for voice user drag)
	v.voiceChanIDs = v.voiceChanIDs[:0]
	for _, vi := range v.visualOrder {
		if vi >= 0 && vi < len(channels) && channels[vi].Type == "voice" {
			v.voiceChanIDs = append(v.voiceChanIDs, channels[vi].ID)
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Server name header with settings gear
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if serverDesc == "" {
							lbl := material.Body1(v.app.Theme.Material, serverName)
							lbl.Color = ColorText
							lbl.MaxLines = 1
							return lbl.Layout(gtx)
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Body1(v.app.Theme.Material, serverName)
								lbl.Color = ColorText
								lbl.MaxLines = 1
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(v.app.Theme.Material, serverDesc)
									lbl.Color = ColorTextDim
									lbl.MaxLines = 2
									return lbl.Layout(gtx)
								})
							}),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						// Show mark-read only if there are unreads
						v.app.mu.RLock()
						hasUnread := len(conn.UnreadCount) > 0 || len(conn.UnreadDMCount) > 0 || len(conn.UnreadGroups) > 0
						v.app.mu.RUnlock()
						if !hasUnread {
							return layout.Dimensions{}
						}
						return v.layoutHeaderIconBtn(gtx, &v.markReadBtn, IconCheck)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutHeaderIconBtn(gtx, &v.createBtn, IconAdd)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return v.layoutSettingsGear(gtx)
					}),
				)
			})
		}),

		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
			paint.FillShape(gtx.Ops, ColorDivider, clip.Rect{Max: size}.Op())
			return layout.Dimensions{Size: size}
		}),

		// Files button
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutIconTextBtn(gtx, v.app.Theme, &v.libraryBtn, IconFolder, "Files", false)
			})
		}),

		// (Whiteboard moved to voice channel only)

		// Servers button (only if game servers enabled)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			conn := v.app.Conn()
			if conn == nil || !conn.GameServersEnabled {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutIconTextBtn(gtx, v.app.Theme, &v.gameServersBtn, IconMonitor, "Servers", v.app.Mode == ViewGameServers)
			})
		}),

		// Channel list
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			listDims := material.List(v.app.Theme.Material, &v.list).Layout(gtx, groupCount, func(gtx layout.Context, idx int) layout.Dimensions {
				if idx >= len(renderItems) {
					return layout.Dimensions{}
				}
				item := renderItems[idx]
				if !item.isCat {
					// Uncategorized block
					dims := v.layoutChannelList(gtx, item.chans, channels, activeID)
					for len(v.renderItemHeights) <= idx {
						v.renderItemHeights = append(v.renderItemHeights, 0)
					}
					v.renderItemHeights[idx] = dims.Size.Y
					return dims
				}

				catID := item.catID
				g := catMap[catID]
				ci := item.catIdx // index in renderedCatIDs

				var items []layout.FlexChild

				// Gap drop line before first category
				if ci == 0 && v.isPreFirstGapDropTarget() {
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutDropLine(gtx)
						})
					}))
				}


				dragIdxLocal := ci // renderedCatIDs index for D&D
				if len(g.children) > 0 {
					// Root category with children
					catIdxLocal := catIdxMap[catID]
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						dims := v.layoutRootCategory(gtx, catID, g.name, g.hexColor, g.children, g.channels, channels, activeID, catIdxLocal, dragIdxLocal)
						if dragIdxLocal < len(v.catHeights) {
							v.catHeights[dragIdxLocal] = dims.Size.Y
						}
						return dims
					}))
				} else {
					// Standalone category (no children)
					catIdxLocal := catIdxMap[catID]
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						dims := v.layoutCategory(gtx, catID, g.name, g.hexColor, g.channels, channels, activeID, catIdxLocal)
						if dragIdxLocal < len(v.catHeights) {
							v.catHeights[dragIdxLocal] = dims.Size.Y
						}
						return dims
					}))
				}

				// Channel drag gap drop line between categories
				if v.isGapDropTarget(ci) {
					items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return v.layoutDropLine(gtx)
						})
					}))
				}

				dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
				for len(v.renderItemHeights) <= idx {
					v.renderItemHeights = append(v.renderItemHeights, 0)
				}
				v.renderItemHeights[idx] = dims.Size.Y
				return dims
			})

			return listDims
		}),

	)
}


// layoutRootCategory — root category with child categories
func (v *ChannelView) layoutRootCategory(gtx layout.Context, catID, name, hexColor string, children []chanCatChild, rootChans []int, channels []api.Channel, activeID string, catIdx, dragIdx int) layout.Dimensions {
	catColor := parseHexColor(hexColor)
	borderColor := withAlpha(catColor, 170)
	isDropTarget := v.isEmptyCatDropTarget(catID)
	if isDropTarget {
		borderColor = ColorAccent
	}
	isCatDragSource := v.catDragging && v.catDragFromID == catID
	if isCatDragSource {
		borderColor = withAlpha(borderColor, 80)
	}
	// Adopt highlight — green border when a category is being dragged over it
	if v.catDragging && v.catDragAction == "adopt" && v.catDragTargetID == catID {
		borderColor = ColorAccent
	}

	// Drop line for cat reorder
	catReorderAbove := false
	catReorderBelow := false
	if v.catDragging && v.catDragAction == "reorder" && v.catDragTargetIdx >= 0 && v.catDragTargetIdx < len(v.renderedCatIDs) && v.renderedCatIDs[v.catDragTargetIdx] == catID && catID != v.catDragFromID {
		if v.catDragTargetIdx < v.catDragFromIdx {
			catReorderAbove = true
		} else {
			catReorderBelow = true
		}
	}

	collapsed := v.collapsedCats[catID]

	return layout.Inset{Top: unit.Dp(10), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		var flexItems []layout.FlexChild

		if catReorderAbove {
			flexItems = append(flexItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutDropLine(gtx)
				})
			}))
		}

		flexItems = append(flexItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, borderColor, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(6)
							bgColor := ColorCard
							if isCatDragSource {
								bgColor = withAlpha(bgColor, 100)
							}
							paint.FillShape(gtx.Ops, bgColor, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							var items []layout.FlexChild

							// Root category header — larger font, thicker color bar
							items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.layoutRootCatHeader(gtx, catIdx, catID, name, catColor, collapsed)
							}))

							// Channels interleaved with child categories
							if !collapsed {
								dropGap := v.rootGapDropAfterChild(catID)

								// Group root channels by slot
								slotChans := make(map[int][]int)
								for _, ci := range rootChans {
									slot := -1
									if s, ok := v.rootChanSlot[channels[ci].ID]; ok {
										slot = s
										if slot >= len(children) {
											slot = len(children) - 1
										}
									}
									slotChans[slot] = append(slotChans[slot], ci)
								}

								// Channels before first child (slot -1)
								for _, chIdx := range slotChans[-1] {
									chIdx := chIdx
									ch := channels[chIdx]
									items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutChannelItem(gtx, chIdx, ch, activeID)
										})
									}))
								}
								// Drop line before first child
								if dropGap == -1 {
									items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutDropLine(gtx)
										})
									}))
								}

								for childIdx, child := range children {
									ch := child
									items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutCategory(gtx, ch.id, ch.name, ch.hexColor, ch.channels, channels, activeID, ch.widgetIdx)
										})
									}))
									// Channels after this child (slot childIdx)
									for _, chIdx := range slotChans[childIdx] {
										chIdx := chIdx
										chItem := channels[chIdx]
										items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return v.layoutChannelItem(gtx, chIdx, chItem, activeID)
											})
										}))
									}
									// Drop line after this child
									if dropGap == childIdx {
										items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return v.layoutDropLine(gtx)
											})
										}))
									}
								}
								// Bottom spacing so root border doesn't merge with last child
								items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Dimensions{Size: image.Pt(0, gtx.Dp(4))}
								}))
							}

							return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
						},
					)
				})
			},
		)
		}))

		if catReorderBelow {
			flexItems = append(flexItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutDropLine(gtx)
				})
			}))
		}

		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, flexItems...)
	})
}

// layoutRootCatHeader — header for root category (larger, Body1 font, D&D handle)
func (v *ChannelView) layoutRootCatHeader(gtx layout.Context, catIdx int, catID, name string, catColor color.NRGBA, collapsed bool) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return v.catHeaderBtns[catIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			hovered := v.catHeaderBtns[catIdx].Hovered() ||
				v.catEditBtns2[catIdx].Hovered() ||
				v.catDelBtns2[catIdx].Hovered()

			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				// Collapse indicator
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						icon := IconExpand
						if collapsed {
							icon = IconCollapse
						}
						return layoutIcon(gtx, icon, 18, ColorTextDim)
					})
				}),
				// Color bar (thicker for root)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						size := image.Pt(gtx.Dp(4), gtx.Dp(18))
						paint.FillShape(gtx.Ops, catColor, clip.Rect{Max: size}.Op())
						return layout.Dimensions{Size: size}
					})
				}),
				// Name (Body1 — larger)
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body1(v.app.Theme.Material, name)
						lbl.Color = catColor
						lbl.MaxLines = 1
						return lbl.Layout(gtx)
					})
				}),
				// Action buttons (edit, delete, drag handle) — visible on hover
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					editColor := ColorAccent
					delColor := ColorDanger
					handleColor := ColorTextDim
					if !hovered {
						editColor = color.NRGBA{}
						delColor = color.NRGBA{}
						handleColor = color.NRGBA{}
					}
					if v.catDragging && v.catDragFromID == catID {
						handleColor = ColorAccent
					}

					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						// Drag handle
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								size := image.Pt(gtx.Dp(16), gtx.Dp(20))
								return layout.Stack{}.Layout(gtx,
									layout.Expanded(func(gtx layout.Context) layout.Dimensions {
										gtx.Constraints.Min = size
										gtx.Constraints.Max = size
										defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
										isDragSource := v.catDragging && v.catDragFromID == catID
										if hovered || isDragSource {
											v.catDrags[catIdx].Add(gtx.Ops)
											if v.catDrags[catIdx].Dragging() {
												pointer.CursorGrabbing.Add(gtx.Ops)
											} else {
												pointer.CursorGrab.Add(gtx.Ops)
											}
										}
										return layout.Dimensions{Size: size}
									}),
									layout.Stacked(func(gtx layout.Context) layout.Dimensions {
										return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconDragHandle, 14, handleColor)
										})
									}),
								)
							})
						}),
						// Edit button
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.catEditBtns2[catIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconEdit, 16, editColor)
							})
						}),
						// Delete button
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.catDelBtns2[catIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconDelete, 16, delColor)
								})
							})
						}),
					)
				}),
			)
		})
	})
}

// isGapDropTarget — is the gap after category at catIndex in renderedCatIDs a drop target?
// Gap 0 = before first category, gap catIndex+1 = after renderedCatIDs[catIndex]
func (v *ChannelView) isGapDropTarget(catIndex int) bool {
	return v.gapDropNum() == catIndex+1
}

// isPreFirstGapDropTarget — is the gap before the first category a drop target?
func (v *ChannelView) isPreFirstGapDropTarget() bool {
	return v.gapDropNum() == 0
}

// gapDropNum — returns the gap number being dropped onto (-1 = no gap)
func (v *ChannelView) gapDropNum() int {
	if !v.dragging || v.dragTargetIdx >= 0 {
		return -1
	}
	emptyCatPos := -(v.dragTargetIdx + 1)
	if emptyCatPos < 0 || emptyCatPos >= len(v.emptyCatIDs) {
		return -1
	}
	if v.emptyCatIDs[emptyCatPos] != "" {
		return -1 // empty cat, not gap
	}
	gapNum := 0
	for i := 0; i <= emptyCatPos; i++ {
		if v.emptyCatIDs[i] == "" {
			if i == emptyCatPos {
				return gapNum
			}
			gapNum++
		}
	}
	return -1
}

func (v *ChannelView) isEmptyCatDropTarget(catID string) bool {
	if !v.dragging || v.dragTargetIdx >= 0 {
		return false
	}
	emptyCatPos := -(v.dragTargetIdx + 1)
	if emptyCatPos >= 0 && emptyCatPos < len(v.emptyCatIDs) {
		return v.emptyCatIDs[emptyCatPos] == catID
	}
	return false
}

// rootGapDropAfterChild returns the slot being targeted for a root category drop.
// Returns -1 for "before first child", 0+ for "after child[N]",
// -2 for "no active root gap drop for this catID".
func (v *ChannelView) rootGapDropAfterChild(catID string) int {
	if !v.dragging || v.dragTargetIdx >= 0 {
		return -2
	}
	emptyCatPos := -(v.dragTargetIdx + 1)
	if emptyCatPos < 0 || emptyCatPos >= len(v.emptyCatIDs) {
		return -2
	}
	if v.emptyCatIDs[emptyCatPos] != catID {
		return -2
	}
	if gapIdx, ok := v.rootGapSentinels[emptyCatPos]; ok {
		return gapIdx
	}
	return -2
}


func (v *ChannelView) layoutCategory(gtx layout.Context, catID, name, hexColor string, indices []int, channels []api.Channel, activeID string, catIdx int) layout.Dimensions {
	catColor := parseHexColor(hexColor)
	borderColor := withAlpha(catColor, 170)
	isDropTarget := v.isEmptyCatDropTarget(catID)
	if isDropTarget {
		borderColor = ColorAccent
	}
	isCatDragSource := v.catDragging && v.catDragFromID == catID
	if isCatDragSource {
		borderColor = withAlpha(borderColor, 80)
	}
	// Adopt highlight — green border when a category is being dragged over it
	if v.catDragging && v.catDragAction == "adopt" && v.catDragTargetID == catID {
		borderColor = ColorAccent
	}

	// Drop line for cat reorder (top-level or child)
	catReorderAbove := false
	catReorderBelow := false
	if v.catDragging && v.catDragAction == "reorder" && v.catDragTargetIdx >= 0 && v.catDragTargetIdx < len(v.renderedCatIDs) && v.renderedCatIDs[v.catDragTargetIdx] == catID && catID != v.catDragFromID {
		if v.catDragTargetIdx < v.catDragFromIdx {
			catReorderAbove = true
		} else {
			catReorderBelow = true
		}
	}
	// Child reorder within same parent
	if v.catDragging && v.catDragAction == "child_reorder" && catID != v.catDragFromID && len(v.catDragSiblings) > 0 {
		if v.catDragChildTo >= 0 && v.catDragChildTo < len(v.catDragSiblings) && v.catDragSiblings[v.catDragChildTo] == catID {
			if v.catDragChildTo < v.catDragChildFrom {
				catReorderAbove = true
			} else {
				catReorderBelow = true
			}
		}
	}

	collapsed := v.collapsedCats[catID]

	// Check if category has channels (for disable delete)
	hasChannels := len(indices) > 0

	return layout.Inset{Top: unit.Dp(10), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		var flexItems []layout.FlexChild

		if catReorderAbove {
			flexItems = append(flexItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutDropLine(gtx)
				})
			}))
		}

		flexItems = append(flexItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
				rr := gtx.Dp(8)
				paint.FillShape(gtx.Ops, borderColor, clip.RRect{
					Rect: bounds,
					NE:   rr, NW: rr, SE: rr, SW: rr,
				}.Op(gtx.Ops))
				return layout.Dimensions{Size: bounds.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(1), Right: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							bounds := image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y)
							rr := gtx.Dp(7)
							bgColor := ColorCard
							if isCatDragSource {
								bgColor = withAlpha(bgColor, 100)
							}
							paint.FillShape(gtx.Ops, bgColor, clip.RRect{
								Rect: bounds,
								NE:   rr, NW: rr, SE: rr, SW: rr,
							}.Op(gtx.Ops))
							return layout.Dimensions{Size: bounds.Max}
						},
						func(gtx layout.Context) layout.Dimensions {
							var items []layout.FlexChild

							// Category header
							items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return v.layoutCatNormalHeader(gtx, catIdx, catID, name, catColor, hasChannels, collapsed)
							}))

							// Channel list or empty hint (hide if collapsed)
							if !collapsed {
								if len(indices) > 0 {
									items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.layoutChannelList(gtx, indices, channels, activeID)
										})
									}))
								} else {
									catIDcopy := catID
									items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										isDT := v.isEmptyCatDropTarget(catIDcopy)
										return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											if isDT {
												r := clip.Rect{Max: gtx.Constraints.Max}.Op()
												paint.FillShape(gtx.Ops, withAlpha(ColorAccent, 30), r)
											}
											return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(7)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Caption(v.app.Theme.Material, "No channels")
												if isDT {
													lbl.Color = ColorAccent
												} else {
													lbl.Color = withAlpha(ColorTextDim, 120)
												}
												return lbl.Layout(gtx)
											})
										})
									}))
								}
							}

							return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
						},
					)
				})
			},
		)
		}))

		if catReorderBelow {
			flexItems = append(flexItems, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return v.layoutDropLine(gtx)
				})
			}))
		}

		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, flexItems...)
	})
}

// layoutCatNormalHeader — normal header with collapse, edit, delete, drag
func (v *ChannelView) layoutCatNormalHeader(gtx layout.Context, catIdx int, catID, name string, catColor color.NRGBA, hasChannels, collapsed bool) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return v.catHeaderBtns[catIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			hovered := v.catHeaderBtns[catIdx].Hovered() ||
				v.catEditBtns2[catIdx].Hovered() ||
				v.catDelBtns2[catIdx].Hovered()

			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				// Collapse indicator
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						icon := IconExpand
						if collapsed {
							icon = IconCollapse
						}
						return layoutIcon(gtx, icon, 16, ColorTextDim)
					})
				}),
				// Color bar
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						size := image.Pt(gtx.Dp(3), gtx.Dp(14))
						paint.FillShape(gtx.Ops, catColor, clip.Rect{Max: size}.Op())
						return layout.Dimensions{Size: size}
					})
				}),
				// Name
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Caption(v.app.Theme.Material, name)
						lbl.Color = ColorTextDim
						lbl.MaxLines = 1
						return lbl.Layout(gtx)
					})
				}),
				// Action buttons — always render, transparent if not hovered
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					editColor := ColorAccent
					delColor := ColorDanger
					handleColor := ColorTextDim
					if !hovered {
						editColor = color.NRGBA{}
						delColor = color.NRGBA{}
						handleColor = color.NRGBA{}
					} else if hasChannels {
						delColor = withAlpha(ColorTextDim, 80) // disabled
					}
					if v.catDragging && v.catDragFromID == catID {
						handleColor = ColorAccent
					}

					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						// Drag handle
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								size := image.Pt(gtx.Dp(16), gtx.Dp(20))
								return layout.Stack{}.Layout(gtx,
									layout.Expanded(func(gtx layout.Context) layout.Dimensions {
										gtx.Constraints.Min = size
										gtx.Constraints.Max = size
										defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
										isDragSrc := v.catDragging && v.catDragFromID == catID
										if hovered || isDragSrc {
											v.catDrags[catIdx].Add(gtx.Ops)
											if v.catDrags[catIdx].Dragging() {
												pointer.CursorGrabbing.Add(gtx.Ops)
											} else {
												pointer.CursorGrab.Add(gtx.Ops)
											}
										}
										return layout.Dimensions{Size: size}
									}),
									layout.Stacked(func(gtx layout.Context) layout.Dimensions {
										return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconDragHandle, 14, handleColor)
										})
									}),
								)
							})
						}),
						// Edit button
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return v.catEditBtns2[catIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layoutIcon(gtx, IconEdit, 14, editColor)
							})
						}),
						// Delete button
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return v.catDelBtns2[catIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconDelete, 14, delColor)
								})
							})
						}),
					)
				}),
			)
		})
	})
}

// computeCatDragTarget — cumulative heights + nearest (same principle as channel drag)
func (v *ChannelView) computeCatDragTarget() {
	n := len(v.renderedCatIDs)
	if n == 0 {
		return
	}

	v.catDragAction = ""
	v.catDragTargetID = ""
	v.catDragTargetIdx = -1
	v.catDragSiblings = nil

	var fromInfo *catDragInfo
	for i := range v.allCatInfos {
		if v.allCatInfos[i].id == v.catDragFromID {
			fromInfo = &v.allCatInfos[i]
			break
		}
	}
	if fromInfo == nil {
		return
	}

	// Child-to-child reorder within the same parent
	if fromInfo.parentID != "" {
		var siblings []string
		for _, ci := range v.allCatInfos {
			if ci.parentID == fromInfo.parentID {
				siblings = append(siblings, ci.id)
			}
		}
		if len(siblings) < 2 {
			return
		}
		fromIdx := -1
		for i, id := range siblings {
			if id == v.catDragFromID {
				fromIdx = i
				break
			}
		}
		if fromIdx < 0 {
			return
		}
		// Use drag offset and estimated child height to determine target
		stepPx := 60
		fromDragIdx := fromInfo.dragIdx
		if fromDragIdx < len(v.catHeights) && v.catHeights[fromDragIdx] > 0 {
			stepPx = v.catHeights[fromDragIdx]
		}
		steps := int(v.catDragOffsetY) / (stepPx / 2)
		toIdx := fromIdx + steps
		if toIdx < 0 {
			toIdx = 0
		}
		if toIdx >= len(siblings) {
			toIdx = len(siblings) - 1
		}
		if toIdx != fromIdx {
			v.catDragAction = "child_reorder"
			v.catDragChildFrom = fromIdx
			v.catDragChildTo = toIdx
			v.catDragSiblings = siblings
		}
		return
	}

	startIdx := v.catDragFromIdx

	// Build cumulative pixel positions from ALL render items (categories + uncategorized blocks)
	// This ensures physical distances match cursor movement
	nRI := len(v.renderItemHeights)
	allCumPx := make([]int, nRI+1)
	for i := 0; i < nRI; i++ {
		h := v.renderItemHeights[i]
		if h <= 0 {
			h = 50
		}
		allCumPx[i+1] = allCumPx[i] + h
	}

	// Extract category positions from allCumPx using catRenderIdx
	catCumPx := make([]int, n)
	for ci := 0; ci < n; ci++ {
		if ci < len(v.catRenderIdx) {
			riIdx := v.catRenderIdx[ci]
			if riIdx < len(allCumPx) {
				catCumPx[ci] = allCumPx[riIdx]
			}
		}
	}

	// Target pixel position = source position + drag offset
	targetPx := catCumPx[startIdx] + int(v.catDragOffsetY)

	// Find nearest category
	target := startIdx
	bestDist := 999999
	for i := 0; i < n; i++ {
		dist := targetPx - catCumPx[i]
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			bestDist = dist
			target = i
		}
	}

	targetCatID := v.renderedCatIDs[target]
	if targetCatID == v.catDragFromID {
		return
	}

	// Adopt detection
	var targetInfo *catDragInfo
	for i := range v.allCatInfos {
		if v.allCatInfos[i].id == targetCatID {
			targetInfo = &v.allCatInfos[i]
			break
		}
	}

	canAdopt := false
	if targetInfo != nil {
		if targetInfo.parentID == "" && !fromInfo.hasChildren && targetCatID != fromInfo.parentID {
			canAdopt = true
		}
	}

	if canAdopt {
		catH := 60
		if target < len(v.catHeights) && v.catHeights[target] > 0 {
			catH = v.catHeights[target]
		}
		// Adopt zone: cursor is deep inside the target (close to its top)
		// distance from top of target — gets smaller as cursor moves deeper
		edge := float32(targetPx - catCumPx[target])
		if edge < 0 {
			edge = -edge
		}
		adoptZone := float32(catH) * 0.3
		if edge < adoptZone {
			v.catDragAction = "adopt"
			v.catDragTargetID = targetCatID
			v.catDragTargetIdx = target
			return
		}
	}

	v.catDragAction = "reorder"
	v.catDragTargetIdx = target
}

// executeCatDrop — performs reorder or reparent of categories after drop
func (v *ChannelView) executeCatDrop() {
	conn := v.app.Conn()
	action := v.catDragAction
	fromID := v.catDragFromID
	fromParent := v.catDragFromParent
	fromIdx := v.catDragFromIdx
	toIdx := v.catDragTargetIdx
	adoptTargetID := v.catDragTargetID
	childFrom := v.catDragChildFrom
	childTo := v.catDragChildTo
	siblings := v.catDragSiblings

	v.catDragging = false
	v.catDragFromIdx = -1
	v.catDragFromID = ""
	v.catDragFromParent = ""
	v.catDragOffsetY = 0
	v.catDragTargetIdx = -1
	v.catDragTargetID = ""
	v.catDragAction = ""
	v.catDragSiblings = nil

	if conn == nil || fromID == "" || action == "" {
		return
	}

	if action == "child_reorder" && len(siblings) > 1 && childFrom != childTo {
		// Reorder children within the same parent
		reordered := make([]string, len(siblings))
		copy(reordered, siblings)
		// Move childFrom to childTo position
		id := reordered[childFrom]
		reordered = append(reordered[:childFrom], reordered[childFrom+1:]...)
		if childTo >= len(reordered) {
			reordered = append(reordered, id)
		} else {
			reordered = append(reordered[:childTo+1], reordered[childTo:]...)
			reordered[childTo] = id
		}
		// Update local state
		v.app.mu.Lock()
		for pi := range conn.Categories {
			if conn.Categories[pi].ID == fromParent {
				newChildren := make([]api.ChannelCategory, 0, len(reordered))
				childMap := make(map[string]api.ChannelCategory)
				for _, ch := range conn.Categories[pi].Children {
					childMap[ch.ID] = ch
				}
				for _, rid := range reordered {
					if ch, ok := childMap[rid]; ok {
						newChildren = append(newChildren, ch)
					}
				}
				conn.Categories[pi].Children = newChildren
				break
			}
		}
		v.app.mu.Unlock()
		go func() {
			if err := conn.Client.ReorderCategories(reordered); err != nil {
				log.Printf("ReorderCategories(children): %v", err)
			}
		}()
		v.app.Window.Invalidate()
		return
	}

	if action == "adopt" && adoptTargetID != "" {
		// Adopt — set parent to target category
		go func() {
			if err := conn.Client.SetCategoryParent(fromID, adoptTargetID); err != nil {
				log.Printf("SetCategoryParent: %v", err)
			}
		}()
		v.app.Window.Invalidate()
		return
	}

	// Reorder (top-level reorder, or child → unadopt + reorder)
	if toIdx < 0 {
		return
	}

	v.app.mu.Lock()
	cats := conn.Categories

	if fromParent != "" {
		// Child category → unadopt (become top-level) + insert at toIdx
		// First remove from parent children (locally)
		for pi, p := range cats {
			if p.ID == fromParent {
				var newChildren []api.ChannelCategory
				var moved api.ChannelCategory
				found := false
				for _, ch := range p.Children {
					if ch.ID == fromID {
						moved = ch
						found = true
					} else {
						newChildren = append(newChildren, ch)
					}
				}
				if !found {
					v.app.mu.Unlock()
					return
				}
				cats[pi].Children = newChildren
				// Insert as top-level at position toIdx
				moved.ParentID = nil
				newCats := make([]api.ChannelCategory, 0, len(cats)+1)
				for i, c := range cats {
					if i == toIdx {
						newCats = append(newCats, moved)
					}
					newCats = append(newCats, c)
				}
				if toIdx >= len(cats) {
					newCats = append(newCats, moved)
				}
				conn.Categories = newCats
				ids := make([]string, len(newCats))
				for i, c := range newCats {
					ids[i] = c.ID
				}
				v.app.mu.Unlock()
				go func() {
					// Unadopt (parent_id = "")
					if err := conn.Client.SetCategoryParent(fromID, ""); err != nil {
						log.Printf("SetCategoryParent(unadopt): %v", err)
					}
					if err := conn.Client.ReorderCategories(ids); err != nil {
						log.Printf("ReorderCategories: %v", err)
					}
				}()
				v.app.Window.Invalidate()
				return
			}
		}
		v.app.mu.Unlock()
		return
	}

	// Top-level reorder
	if fromIdx < 0 || fromIdx == toIdx || fromIdx >= len(cats) || toIdx >= len(cats) {
		v.app.mu.Unlock()
		return
	}

	cat := cats[fromIdx]
	newCats := make([]api.ChannelCategory, 0, len(cats))
	for i, c := range cats {
		if i == fromIdx {
			continue
		}
		if i == toIdx {
			if fromIdx > toIdx {
				newCats = append(newCats, cat)
			}
			newCats = append(newCats, c)
			if fromIdx < toIdx {
				newCats = append(newCats, cat)
			}
		} else {
			newCats = append(newCats, c)
		}
	}
	conn.Categories = newCats

	ids := make([]string, len(newCats))
	for i, c := range newCats {
		ids[i] = c.ID
	}
	v.app.mu.Unlock()

	go func() {
		if err := conn.Client.ReorderCategories(ids); err != nil {
			log.Printf("ReorderCategories: %v", err)
		}
	}()
	v.app.Window.Invalidate()
}

func (v *ChannelView) layoutChannelList(gtx layout.Context, indices []int, channels []api.Channel, activeID string) layout.Dimensions {
	var children []layout.FlexChild
	for _, idx := range indices {
		idx := idx
		ch := channels[idx]
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutChannelItem(gtx, idx, ch, activeID)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (v *ChannelView) layoutChannelItem(gtx layout.Context, idx int, ch api.Channel, activeID string) layout.Dimensions {
	// Drag events are processed at Layout() level (before list scroll)

	active := ch.ID == activeID
	isVoice := ch.Type == "voice"
	isLobby := ch.Type == "lobby"
	isLAN := ch.Type == "lan"
	isDragSource := v.dragging && v.dragFromID == ch.ID
	isDropTarget := v.dragging && v.dragFromID != ch.ID && idx == v.dragTargetIdx

	// Get unread count, voice users, screen sharers, and LAN members
	conn := v.app.Conn()
	var unread int
	var voiceUsers []string
	var screenSharers map[string]string
	var lanMembers []api.LANPartyMember
	if conn != nil {
		v.app.mu.RLock()
		unread = conn.UnreadCount[ch.ID]
		if isLAN {
			lanMembers = conn.LANMembers[ch.ID]
		}
		if isVoice {
			voiceUsers = conn.VoiceState[ch.ID]
			screenSharers = conn.ScreenSharers
		}
		v.app.mu.RUnlock()
	}

	// Clicked() internally calls Update() → updates hover state.
	// Therefore process clicks FIRST, then read Hovered().
	chanClicked := v.chanBtns[idx].Clicked(gtx)
	editClicked := v.chanEditBtns[idx].Clicked(gtx)
	deleteClicked := v.chanDelBtns[idx].Clicked(gtx)

	hovered := v.chanBtns[idx].Hovered() || v.chanDelBtns[idx].Hovered() ||
		v.chanEditBtns[idx].Hovered()
	showActions := hovered || isDragSource

	if chanClicked && !v.dragging {
		if ch.Type == "" || ch.Type == "text" {
			v.app.SelectChannel(ch.ID, ch.Name)
		} else if ch.Type == "voice" {
			if c := v.app.Conn(); c != nil && c.Voice != nil {
				if c.Call != nil && c.Call.IsActive() {
					c.Call.HangupCall()
				}
				c.Voice.Join(ch.ID)
			}
		} else if ch.Type == "lobby" {
			// Open lobby join dialog with pre-filled prefs from cache
			if c := v.app.Conn(); c != nil {
				prefs := store.GetLobbyPrefs(v.app.PublicKey, c.URL, ch.ID)
				v.app.LobbyJoinDlg.Show(ch.ID, ch.Name, prefs)
			}
		} else if ch.Type == "lan" {
			chID := ch.ID
			go v.app.LANHelper.ToggleLAN(conn, chID)
		} else if ch.Type == "lfg" {
			v.app.SelectChannel(ch.ID, ch.Name)
		}
	}

	if editClicked && showActions {
		v.app.ChannelEditDlg.Show(ch)
	}

	if deleteClicked && showActions {
		chID := ch.ID
		chName := ch.Name
		v.app.ConfirmDlg.ShowWithText(
			"Delete Channel",
			"Are you sure you want to delete #"+chName+"? This cannot be undone.",
			"Delete",
			func() {
				if c := v.app.Conn(); c != nil {
					if err := c.Client.DeleteChannel(chID); err != nil {
						log.Printf("DeleteChannel: %v", err)
						return
					}
					v.app.mu.Lock()
					for i, ch := range c.Channels {
						if ch.ID == chID {
							c.Channels = append(c.Channels[:i], c.Channels[i+1:]...)
							break
						}
					}
					if c.ActiveChannelID == chID {
						c.ActiveChannelID = ""
						c.ActiveChannelName = ""
						c.Messages = nil
					}
					v.app.mu.Unlock()
					v.app.Window.Invalidate()
				}
			},
		)
	}

	var items []layout.FlexChild

	// Drop indicator line (above this item) — use visual order for direction
	dragFromVisual := v.visualPos(v.dragFromIdx)
	dragToVisual := v.visualPos(idx)
	if isDropTarget && dragFromVisual > dragToVisual {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutDropLine(gtx)
		}))
	}

	// Voice drag highlight — target voice channel
	isVoiceDragTarget := v.voiceDragging && isVoice && ch.ID == v.voiceDragTargetChan && ch.ID != v.voiceDragFromChanID

	// Channel button row
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return v.chanBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			bg := ColorCard
			if active {
				bg = ColorSelected
			} else if hovered || isDropTarget {
				bg = ColorHover
			}
			if isDragSource {
				bg = withAlpha(bg, 100)
			}
			if isVoiceDragTarget {
				bg = withAlpha(ColorAccent, 60)
			}

			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
					paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
					return layout.Dimensions{Size: bounds.Max}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								textColor := ColorTextDim
								if active {
									textColor = ColorText
								} else if unread > 0 {
									textColor = ColorText
								}
								if isVoice {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconVolumeUp, 16, textColor)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Body2(v.app.Theme.Material, ch.Name)
												lbl.Color = textColor
												if !active && unread > 0 {
													lbl.Font.Weight = 700
												}
												return lbl.Layout(gtx)
											})
										}),
									)
								}
								if isLobby {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconAdd, 14, textColor)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconVolumeUp, 14, textColor)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Body2(v.app.Theme.Material, ch.Name)
												lbl.Color = textColor
												return lbl.Layout(gtx)
											})
										}),
									)
								}
								if isLAN {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconLink, 16, textColor)
										}),
										// Helper status dot
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											sz := gtx.Dp(6)
											dotColor := ColorDanger
											if v.app.LANHelper != nil && v.app.LANHelper.IsOK() {
												dotColor = ColorSuccess
											}
											return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												paint.FillShape(gtx.Ops, dotColor, clip.Ellipse{Max: image.Pt(sz, sz)}.Op(gtx.Ops))
												return layout.Dimensions{Size: image.Pt(sz, sz)}
											})
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Body2(v.app.Theme.Material, ch.Name)
												lbl.Color = textColor
												return lbl.Layout(gtx)
											})
										}),
									)
								}
								isLFG := ch.Type == "lfg"
								if isLFG {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconGroup, 16, textColor)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Body2(v.app.Theme.Material, ch.Name)
												lbl.Color = textColor
												if !active && unread > 0 {
													lbl.Font.Weight = 700
												}
												return lbl.Layout(gtx)
											})
										}),
									)
								}
								lbl := material.Body2(v.app.Theme.Material, "# "+ch.Name)
								lbl.Color = textColor
								if !active && unread > 0 {
									lbl.Font.Weight = 700
								}
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								// Always render buttons with same layout — transparent if not hovered
								editColor := ColorAccent
								delColor := ColorDanger
								handleColor := ColorTextDim
								if !showActions {
									editColor = color.NRGBA{}
									delColor = color.NRGBA{}
									handleColor = color.NRGBA{}
								}
								if v.dragging && v.dragFromIdx == idx {
									handleColor = ColorAccent
								}

								return layout.Flex{}.Layout(gtx,
									// Drag handle — always present for stable layout
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											size := image.Pt(gtx.Dp(16), gtx.Dp(20))
											return layout.Stack{}.Layout(gtx,
												layout.Expanded(func(gtx layout.Context) layout.Dimensions {
													gtx.Constraints.Min = size
													gtx.Constraints.Max = size
													defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
													if showActions {
														v.chanDrags[idx].Add(gtx.Ops)
														if v.chanDrags[idx].Dragging() {
															pointer.CursorGrabbing.Add(gtx.Ops)
														} else {
															pointer.CursorGrab.Add(gtx.Ops)
														}
													}
													return layout.Dimensions{Size: size}
												}),
												layout.Stacked(func(gtx layout.Context) layout.Dimensions {
													return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														return layoutIcon(gtx, IconDragHandle, 12, handleColor)
													})
												}),
											)
										})
									}),
									// Edit button
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return v.chanEditBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutIcon(gtx, IconEdit, 14, editColor)
										})
									}),
									// Delete button
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return v.chanDelBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return layoutIcon(gtx, IconDelete, 14, delColor)
											})
										})
									}),
								)
							}),
						)
					})
				},
			)
		})
	}))

	// Drop indicator line (below this item)
	if isDropTarget && dragFromVisual < dragToVisual {
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.layoutDropLine(gtx)
		}))
	}

	// Voice users under voice channel
	if isVoice && len(voiceUsers) > 0 {
		// Get speaking states from voice manager
		var peerSpeaking map[string]bool
		var selfSpeaking bool
		if conn != nil && conn.Voice != nil && conn.Voice.IsActive() {
			selfSpeaking, peerSpeaking = conn.Voice.GetSpeakingState()
		}

		// Check permissions for drag (PermKick)
		canDragVoice := conn != nil && conn.MyPermissions&api.PermKick != 0

		for _, uid := range voiceUsers {
			userID := uid
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// Dim the user being moved
				isVoiceDragSource := v.voiceDragging && v.voiceDragUserID == userID

				return layout.Inset{Left: unit.Dp(28), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					name := userID[:8]
					if u := v.app.FindUser(userID); u != nil {
						name = v.app.ResolveUserName(u)
					}

					// Determine if this user is speaking
					speaking := false
					if conn != nil && userID == conn.UserID {
						speaking = selfSpeaking
					} else if peerSpeaking != nil {
						speaking = peerSpeaking[userID]
					}

					isSharing := screenSharers[userID] != ""

					// Click handler → open UserPopup
					vuBtn := v.getVoiceUserBtn(userID)
					if vuBtn.Clicked(gtx) && !v.voiceDragging {
						v.app.UserPopup.Show(userID, name)
					}

					// Stream icon click handler (outside vuBtn so it doesn't bubble)
					var streamClicked bool
					if isSharing {
						btn := v.getStreamBtn(userID)
						if btn.Clicked(gtx) {
							streamClicked = true
							if v.app.StreamViewer != nil && v.app.StreamViewer.Visible && v.app.StreamViewer.StreamerID == userID {
								v.app.StreamViewer.StopWatching()
							} else if v.app.StreamViewer != nil {
								v.app.StreamViewer.StartWatching(userID)
							}
						}
						_ = streamClicked
					}

					// Live WB pen icon click handler
					isLiveWBStarter := conn != nil && conn.LiveWhiteboards[ch.ID] == userID
					if isLiveWBStarter {
						wbBtn := v.getLiveWBBtn(userID)
						if wbBtn.Clicked(gtx) {
							if v.app.LiveWB != nil {
								v.app.LiveWB.Open(ch.ID, userID)
							}
						}
					}

					dims := layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						// User (clickable → UserPopup)
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return vuBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								bg := color.NRGBA{}
								if vuBtn.Hovered() {
									pointer.CursorPointer.Add(gtx.Ops)
									bg = ColorHover
								}
								// Dim the user being moved
								alpha := uint8(255)
								if isVoiceDragSource {
									alpha = 100
								}
								return layout.Background{}.Layout(gtx,
									func(gtx layout.Context) layout.Dimensions {
										bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
										if bg.A > 0 {
											rr := gtx.Dp(4)
											paint.FillShape(gtx.Ops, bg, clip.RRect{
												Rect: bounds,
												NE: rr, NW: rr, SE: rr, SW: rr,
											}.Op(gtx.Ops))
										}
										return layout.Dimensions{Size: bounds.Max}
									},
									func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
												// Speaking indicator dot
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													size := gtx.Dp(6)
													dotColor := withAlpha(ColorTextDim, 80)
													if speaking {
														dotColor = color.NRGBA{R: 80, G: 220, B: 80, A: 255}
													}
													if isVoiceDragSource {
														dotColor.A = alpha
													}
													paint.FillShape(gtx.Ops, dotColor, clip.Ellipse{Max: image.Pt(size, size)}.Op(gtx.Ops))
													return layout.Dimensions{Size: image.Pt(size, size)}
												}),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														lbl := material.Caption(v.app.Theme.Material, name)
														if speaking {
															lbl.Color = color.NRGBA{R: 80, G: 220, B: 80, A: 255}
														} else {
															lbl.Color = ColorSuccess
														}
														if isVoiceDragSource {
															lbl.Color.A = alpha
														}
														return lbl.Layout(gtx)
													})
												}),
											)
										})
									},
								)
							})
						}),
						// Screen share icon (outside vuBtn — separate click handler)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !isSharing {
								return layout.Dimensions{}
							}
							btn := v.getStreamBtn(userID)
							watching := v.app.StreamViewer != nil && v.app.StreamViewer.Visible && v.app.StreamViewer.StreamerID == userID
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									iconColor := ColorAccent
									if watching {
										iconColor = ColorSuccess
									}
									return layoutIcon(gtx, IconMonitor, 14, iconColor)
								})
							})
						}),
						// Live whiteboard pen icon on the starter
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !isLiveWBStarter {
								return layout.Dimensions{}
							}
							wbBtn := v.getLiveWBBtn(userID)
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return wbBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconEdit, 14, ColorAccent)
								})
							})
						}),
					)

					// Drag gesture overlay for voice user (only with PermKick, not self)
					// PassOp lets clicks through to icons (stream, WB) while still capturing drags
					if canDragVoice && conn != nil && userID != conn.UserID {
						drag := v.getVoiceDrag(userID)
						areaStack := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
						passStack := pointer.PassOp{}.Push(gtx.Ops)
						drag.Add(gtx.Ops)
						if drag.Dragging() {
							pointer.CursorGrabbing.Add(gtx.Ops)
						} else {
							pointer.CursorGrab.Add(gtx.Ops)
						}
						passStack.Pop()
						areaStack.Pop()
					}

					return dims
				})
			}))
		}
	}

	// Lobby sub-channels — indented voice channels under lobby
	if isLobby && conn != nil {
		subIndices := v.lobbyChildren[ch.ID]
		for _, si := range subIndices {
			subIdx := si
			if subIdx < len(conn.Channels) {
				subCh := conn.Channels[subIdx]
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return v.layoutLobbySubChannel(gtx, subIdx, subCh)
				}))
			}
		}
	}

	// LAN members under LAN channel
	if isLAN && len(lanMembers) > 0 {
		for _, mem := range lanMembers {
			m := mem
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(28), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					name := m.UserID[:8]
					if m.Username != "" {
						name = m.Username
					}
					vuBtn := v.getVoiceUserBtn(m.UserID)
					if vuBtn.Clicked(gtx) {
						v.app.UserPopup.Show(m.UserID, name)
					}
					return vuBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						bg := color.NRGBA{}
						if vuBtn.Hovered() {
							pointer.CursorPointer.Add(gtx.Ops)
							bg = ColorHover
						}
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
								if bg.A > 0 {
									rr := gtx.Dp(4)
									paint.FillShape(gtx.Ops, bg, clip.RRect{
										Rect: bounds,
										NE:   rr, NW: rr, SE: rr, SW: rr,
									}.Op(gtx.Ops))
								}
								return layout.Dimensions{Size: bounds.Max}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										// Green dot (connected)
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											sz := gtx.Dp(6)
											paint.FillShape(gtx.Ops, ColorSuccess, clip.Ellipse{Max: image.Pt(sz, sz)}.Op(gtx.Ops))
											return layout.Dimensions{Size: image.Pt(sz, sz)}
										}),
										// Username
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Caption(v.app.Theme.Material, name)
												lbl.Color = ColorSuccess
												return lbl.Layout(gtx)
											})
										}),
										// IP address on the right
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Caption(v.app.Theme.Material, m.AssignedIP)
												lbl.Color = ColorTextDim
												lbl.Alignment = 2 // End
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
	}

	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)

	// Record channel height for precise drag targeting
	if v.chanItemH != nil {
		v.chanItemH[idx] = dims.Size.Y
	}

	// Right-click area for notification menu (text channels only, PassOp passes clicks through)
	if (ch.Type == "" || ch.Type == "text") && idx < len(v.chanRightTags) {
		areaStack := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
		pr := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, &v.chanRightTags[idx])
		pr.Pop()
		areaStack.Pop()
	}

	return dims
}

func (v *ChannelView) getStreamBtn(userID string) *widget.Clickable {
	btn, ok := v.streamBtns[userID]
	if !ok {
		btn = &widget.Clickable{}
		v.streamBtns[userID] = btn
	}
	return btn
}

func (v *ChannelView) getLiveWBBtn(userID string) *widget.Clickable {
	btn, ok := v.liveWBBtns[userID]
	if !ok {
		btn = &widget.Clickable{}
		v.liveWBBtns[userID] = btn
	}
	return btn
}

func (v *ChannelView) getVoiceUserBtn(userID string) *widget.Clickable {
	btn, ok := v.voiceUserBtns[userID]
	if !ok {
		btn = &widget.Clickable{}
		v.voiceUserBtns[userID] = btn
	}
	return btn
}

func (v *ChannelView) getSubChannelBtn(channelID string) *widget.Clickable {
	btn, ok := v.subChannelBtns[channelID]
	if !ok {
		btn = &widget.Clickable{}
		v.subChannelBtns[channelID] = btn
	}
	return btn
}

func (v *ChannelView) getVoiceDrag(userID string) *gesture.Drag {
	drag, ok := v.voiceDrags[userID]
	if !ok {
		drag = &gesture.Drag{}
		v.voiceDrags[userID] = drag
	}
	return drag
}

// computeVoiceDragTarget computes target voice channel from drag offset (step-based)
func (v *ChannelView) computeVoiceDragTarget() {
	if len(v.voiceChanIDs) < 2 {
		v.voiceDragTargetChan = ""
		return
	}

	// Find index of source channel in the voice channel list
	fromIdx := -1
	for i, id := range v.voiceChanIDs {
		if id == v.voiceDragFromChanID {
			fromIdx = i
			break
		}
	}
	if fromIdx < 0 {
		v.voiceDragTargetChan = ""
		return
	}

	stepPx := v.dragStepPx
	if stepPx < 1 {
		stepPx = 30
	}
	steps := int(v.voiceDragOffsetY / float32(stepPx))
	targetIdx := fromIdx + steps

	if targetIdx < 0 {
		targetIdx = 0
	}
	if targetIdx >= len(v.voiceChanIDs) {
		targetIdx = len(v.voiceChanIDs) - 1
	}

	v.voiceDragTargetChan = v.voiceChanIDs[targetIdx]
}

// executeVoiceDrop performs moving a user to the target voice channel
func (v *ChannelView) executeVoiceDrop() {
	targetChan := v.voiceDragTargetChan
	userID := v.voiceDragUserID
	fromChan := v.voiceDragFromChanID

	// Reset state
	v.voiceDragging = false
	v.voiceDragUserID = ""
	v.voiceDragFromChanID = ""
	v.voiceDragTargetChan = ""
	v.voiceDragOffsetY = 0

	if targetChan == "" || targetChan == fromChan || userID == "" {
		return
	}

	go func() {
		if c := v.app.Conn(); c != nil {
			if err := c.Client.VoiceMove(userID, targetChan); err != nil {
				log.Printf("VoiceMove: %v", err)
			}
		}
	}()
}

// layoutLobbySubChannel — indented voice sub-channel under lobby
func (v *ChannelView) layoutLobbySubChannel(gtx layout.Context, idx int, ch api.Channel) layout.Dimensions {
	conn := v.app.Conn()
	if conn == nil {
		return layout.Dimensions{}
	}

	btn := v.getSubChannelBtn(ch.ID)
	if btn.Clicked(gtx) {
		if conn.Voice != nil {
			if conn.Call != nil && conn.Call.IsActive() {
				conn.Call.HangupCall()
			}
			conn.Voice.Join(ch.ID)
		}
	}

	hovered := btn.Hovered()

	// Voice users in this sub-channel
	v.app.mu.RLock()
	voiceUsers := conn.VoiceState[ch.ID]
	screenSharers := conn.ScreenSharers
	v.app.mu.RUnlock()

	var items []layout.FlexChild

	// Sub-channel row (indented)
	items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				bg := ColorCard
				if hovered {
					bg = ColorHover
				}
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						bounds := image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Min.Y)
						paint.FillShape(gtx.Ops, bg, clip.Rect{Max: bounds.Max}.Op())
						return layout.Dimensions{Size: bounds.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconVolumeUp, 14, ColorTextDim)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Caption(v.app.Theme.Material, ch.Name)
										lbl.Color = ColorTextDim
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

	// Voice users under sub-channel
	if len(voiceUsers) > 0 {
		var peerSpeaking map[string]bool
		var selfSpeaking bool
		if conn.Voice != nil && conn.Voice.IsActive() {
			selfSpeaking, peerSpeaking = conn.Voice.GetSpeakingState()
		}

		for _, uid := range voiceUsers {
			userID := uid
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(40), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					name := userID[:8]
					if u := v.app.FindUser(userID); u != nil {
						name = v.app.ResolveUserName(u)
					}

					speaking := false
					if userID == conn.UserID {
						speaking = selfSpeaking
					} else if peerSpeaking != nil {
						speaking = peerSpeaking[userID]
					}

					isSharing := screenSharers[userID] != ""

					vuBtn := v.getVoiceUserBtn(userID)
					if vuBtn.Clicked(gtx) {
						v.app.UserPopup.Show(userID, name)
					}

					if isSharing {
						sBtn := v.getStreamBtn(userID)
						if sBtn.Clicked(gtx) {
							if v.app.StreamViewer != nil && v.app.StreamViewer.Visible && v.app.StreamViewer.StreamerID == userID {
								v.app.StreamViewer.StopWatching()
							} else if v.app.StreamViewer != nil {
								v.app.StreamViewer.StartWatching(userID)
							}
						}
					}

					// Live WB pen icon for sub-channel
					isSubLiveWBStarter := conn.LiveWhiteboards[ch.ID] == userID
					if isSubLiveWBStarter {
						wbBtn := v.getLiveWBBtn(userID)
						if wbBtn.Clicked(gtx) {
							if v.app.LiveWB != nil {
								v.app.LiveWB.Open(ch.ID, userID)
							}
						}
					}

					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return vuBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											size := gtx.Dp(6)
											dotColor := withAlpha(ColorTextDim, 80)
											if speaking {
												dotColor = color.NRGBA{R: 80, G: 220, B: 80, A: 255}
											}
											paint.FillShape(gtx.Ops, dotColor, clip.Ellipse{Max: image.Pt(size, size)}.Op(gtx.Ops))
											return layout.Dimensions{Size: image.Pt(size, size)}
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Caption(v.app.Theme.Material, name)
												if speaking {
													lbl.Color = color.NRGBA{R: 80, G: 220, B: 80, A: 255}
												} else {
													lbl.Color = ColorSuccess
												}
												return lbl.Layout(gtx)
											})
										}),
									)
								})
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !isSharing {
								return layout.Dimensions{}
							}
							sBtn := v.getStreamBtn(userID)
							watching := v.app.StreamViewer != nil && v.app.StreamViewer.Visible && v.app.StreamViewer.StreamerID == userID
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return sBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									iconColor := ColorAccent
									if watching {
										iconColor = ColorSuccess
									}
									return layoutIcon(gtx, IconMonitor, 14, iconColor)
								})
							})
						}),
						// Live WB pen icon on sub-channel starter
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !isSubLiveWBStarter {
								return layout.Dimensions{}
							}
							wbBtn := v.getLiveWBBtn(userID)
							return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return wbBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layoutIcon(gtx, IconEdit, 14, ColorAccent)
								})
							})
						}),
					)
				})
			}))
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
}


func (v *ChannelView) layoutDropLine(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(2)
	if h < 1 {
		h = 1
	}
	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := image.Pt(gtx.Constraints.Max.X, h)
		paint.FillShape(gtx.Ops, ColorAccent, clip.Rect{Max: size}.Op())
		return layout.Dimensions{Size: size}
	})
}

func (v *ChannelView) computeDragTarget() {
	if len(v.visualOrder) == 0 {
		return
	}

	// Find drag source position in visual order
	fromVisual := -1
	for i, idx := range v.visualOrder {
		if idx == v.dragFromIdx {
			fromVisual = i
			break
		}
	}
	if fromVisual < 0 {
		return
	}

	stepPx := v.dragStepPx
	if stepPx < 1 {
		stepPx = 30
	}

	// Cumulative pixel positions — actual channel heights + estimated category header heights
	// Sentinel (category header / gap) ~ 34dp ≈ stepPx (32dp)
	gapPx := stepPx
	cumPx := make([]int, len(v.visualOrder))
	total := 0
	for i, vi := range v.visualOrder {
		cumPx[i] = total
		if vi < 0 {
			total += gapPx
		} else {
			h := stepPx // fallback
			if mh, ok := v.chanItemH[vi]; ok && mh > 0 {
				h = mh
			}
			total += h
		}
	}

	// Target pixel position = source position + cursor offset
	targetPx := cumPx[fromVisual] + int(v.dragOffsetY)

	// Find nearest item
	targetVisual := fromVisual
	bestDist := 999999
	for i, px := range cumPx {
		dist := targetPx - px
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			bestDist = dist
			targetVisual = i
		}
	}

	v.dragTargetIdx = v.visualOrder[targetVisual]
}

func (v *ChannelView) executeDrop() {
	conn := v.app.Conn()
	fromIdx := v.dragFromIdx
	toIdx := v.dragTargetIdx
	// Save visual positions before clearing state
	fromVisual := v.visualPos(fromIdx)
	toVisual := v.visualPos(toIdx)
	savedVisualOrder := make([]int, len(v.visualOrder))
	copy(savedVisualOrder, v.visualOrder)

	v.dragging = false
	v.dragFromIdx = -1
	v.dragFromID = ""
	v.dragOffsetY = 0
	v.dragTargetIdx = -1

	if conn == nil || fromIdx < 0 || fromIdx == toIdx {
		return
	}

	// Dropping onto sentinel (empty cat or gap between cats)?
	droppingOnSentinel := toIdx < 0
	var sentinelTargetCatID string
	isGapDrop := false
	if droppingOnSentinel {
		emptyCatPos := -(toIdx + 1)
		if emptyCatPos < 0 || emptyCatPos >= len(v.emptyCatIDs) {
			return
		}
		sentinelTargetCatID = v.emptyCatIDs[emptyCatPos]
		isGapDrop = sentinelTargetCatID == ""
		if isGapDrop {
			// All gaps = uncategorize (channel will be inserted at the gap position)
			sentinelTargetCatID = "_uncategorize_"
		}
	}

	v.app.mu.Lock()
	channels := conn.Channels
	if fromIdx >= len(channels) {
		v.app.mu.Unlock()
		return
	}
	if !droppingOnSentinel && toIdx >= len(channels) {
		v.app.mu.Unlock()
		return
	}

	ch := channels[fromIdx]
	categoryChanged := false

	if droppingOnSentinel {
		// Category change
		if sentinelTargetCatID == "_uncategorize_" {
			if ch.CategoryID != nil {
				ch.CategoryID = nil
				categoryChanged = true
			}
			delete(v.rootChanSlot, ch.ID)
			// Save gap drop position
			// Gap 0 = before first category (slot -1), gap N = after renderedCatIDs[N-1] (slot N-1)
			emptyCatPos := -(toIdx + 1)
			gapNum := 0
			for i := 0; i < emptyCatPos && i < len(v.emptyCatIDs); i++ {
				if v.emptyCatIDs[i] == "" {
					gapNum++
				}
			}
			v.uncatPositions[ch.ID] = gapNum - 1
		} else {
			if !sameCategoryID(ch.CategoryID, &sentinelTargetCatID) {
				ch.CategoryID = &sentinelTargetCatID
				categoryChanged = true
				// Channel was moved into a category → delete from uncatPositions
				delete(v.uncatPositions, ch.ID)
			}
			// Set slot position within root category
			emptyCatPos := -(toIdx + 1)
			if gapIdx, ok := v.rootGapSentinels[emptyCatPos]; ok {
				v.rootChanSlot[ch.ID] = gapIdx
			}
		}

		// Reorder: insert channel at sentinel position in visual order
		newOrder := make([]api.Channel, 0, len(channels))
		inserted := false
		for i, vi := range savedVisualOrder {
			if vi == fromIdx {
				continue // skip source
			}
			if vi < 0 {
				// Insert channel at sentinel position (if it is the target sentinel)
				if i == toVisual && !inserted {
					newOrder = append(newOrder, ch)
					inserted = true
				}
				continue
			}
			newOrder = append(newOrder, channels[vi])
		}
		if !inserted {
			newOrder = append(newOrder, ch)
		}
		// Add channels outside visual order (hidden in collapsed categories etc.)
		visualSet := make(map[int]bool, len(savedVisualOrder))
		for _, vi := range savedVisualOrder {
			if vi >= 0 {
				visualSet[vi] = true
			}
		}
		for i, c := range channels {
			if !visualSet[i] && i != fromIdx {
				newOrder = append(newOrder, c)
			}
		}

		conn.Channels = newOrder
		ids := make([]string, len(newOrder))
		for i, c := range newOrder {
			ids[i] = c.ID
		}
		v.app.mu.Unlock()

		go func() {
			if categoryChanged {
				var catForReq *string
				if ch.CategoryID == nil {
					empty := ""
					catForReq = &empty
				} else {
					catForReq = ch.CategoryID
				}
				conn.Client.UpdateChannel(ch.ID, ch.Name, ch.Topic, catForReq, nil)
			}
			conn.Client.ReorderChannels(ids)
		}()
		v.app.Window.Invalidate()
		return
	}

	// Adopt target's category if different
	targetCatID := channels[toIdx].CategoryID
	if !sameCategoryID(ch.CategoryID, targetCatID) {
		ch.CategoryID = copyCategoryID(targetCatID)
		categoryChanged = true
	}
	// Channel got a category → delete from uncatPositions, adopt slot from target
	delete(v.uncatPositions, ch.ID)
	targetChID := channels[toIdx].ID
	if s, ok := v.rootChanSlot[targetChID]; ok {
		v.rootChanSlot[ch.ID] = s
	} else {
		delete(v.rootChanSlot, ch.ID)
	}

	// Reorder: build new array from visual order with the move applied
	newOrder := make([]api.Channel, 0, len(channels))
	for _, vi := range savedVisualOrder {
		if vi < 0 {
			continue // skip sentinels
		}
		if vi == fromIdx {
			continue // skip source, we'll insert it at target
		}
		if vi == toIdx {
			if fromVisual > toVisual {
				newOrder = append(newOrder, ch)
			}
			newOrder = append(newOrder, channels[vi])
			if fromVisual < toVisual {
				newOrder = append(newOrder, ch)
			}
			continue
		}
		newOrder = append(newOrder, channels[vi])
	}
	// Add channels not in visual order (hidden by collapsed categories, etc.)
	visualSet := make(map[int]bool, len(savedVisualOrder))
	for _, vi := range savedVisualOrder {
		if vi >= 0 {
			visualSet[vi] = true
		}
	}
	for i, c := range channels {
		if !visualSet[i] && i != fromIdx {
			newOrder = append(newOrder, c)
		}
	}

	conn.Channels = newOrder

	// Build new order IDs
	ids := make([]string, len(newOrder))
	for i, c := range newOrder {
		ids[i] = c.ID
	}
	v.app.mu.Unlock()

	// Send to server
	go func() {
		if categoryChanged {
			catForReq := ch.CategoryID
			if catForReq == nil {
				empty := ""
				catForReq = &empty
			}
			if err := conn.Client.UpdateChannel(ch.ID, ch.Name, ch.Topic, catForReq, nil); err != nil {
				log.Printf("UpdateChannel(category): %v", err)
			}
		}
		if err := conn.Client.ReorderChannels(ids); err != nil {
			log.Printf("ReorderChannels: %v", err)
		}
	}()
	v.app.Window.Invalidate()
}

func (v *ChannelView) visualPos(flatIdx int) int {
	for i, vi := range v.visualOrder {
		if vi == flatIdx {
			return i
		}
	}
	return -1
}

func copyCategoryID(cat *string) *string {
	if cat == nil {
		return nil
	}
	v := *cat
	return &v
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

func normalizeCategoryID(cat *string) *string {
	if cat == nil {
		return nil
	}
	if *cat == "" {
		return nil
	}
	v := *cat
	return &v
}

func sameCategoryID(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func parseHexColor(hex string) color.NRGBA {
	if len(hex) == 7 && hex[0] == '#' {
		r := hexByte(hex[1], hex[2])
		g := hexByte(hex[3], hex[4])
		b := hexByte(hex[5], hex[6])
		return color.NRGBA{R: r, G: g, B: b, A: 255}
	}
	if len(hex) == 9 && hex[0] == '#' {
		r := hexByte(hex[1], hex[2])
		g := hexByte(hex[3], hex[4])
		b := hexByte(hex[5], hex[6])
		a := hexByte(hex[7], hex[8])
		return color.NRGBA{R: r, G: g, B: b, A: a}
	}
	return ColorTextDim
}

func hexByte(h, l byte) byte {
	return hexNibble(h)<<4 | hexNibble(l)
}

func hexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func (v *ChannelView) layoutSettingsGear(gtx layout.Context) layout.Dimensions {
	size := gtx.Dp(28)
	return v.settingsBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min

		bg := color.NRGBA{A: 0}
		if v.settingsBtn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			bg = ColorHover
		}

		rr := size / 4
		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layoutIcon(gtx, IconSettings, 18, ColorTextDim)
		})
	})
}

func (v *ChannelView) layoutHeaderIconBtn(gtx layout.Context, btn *widget.Clickable, icon *NIcon) layout.Dimensions {
	size := gtx.Dp(28)
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Pt(size, size)
		gtx.Constraints.Max = gtx.Constraints.Min

		bg := color.NRGBA{A: 0}
		if btn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
			bg = ColorHover
		}

		rr := size / 4
		paint.FillShape(gtx.Ops, bg, clip.RRect{
			Rect: image.Rect(0, 0, size, size),
			NE:   rr, NW: rr, SE: rr, SW: rr,
		}.Op(gtx.Ops))

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layoutIcon(gtx, icon, 18, ColorTextDim)
		})
	})
}

func (v *ChannelView) showChannelNotifyMenu(channelID string, x, y int) {
	a := v.app
	conn := a.Conn()
	if conn == nil {
		return
	}

	a.mu.RLock()
	chLevel, hasOverride := conn.ChannelNotify[channelID]
	serverURL := conn.URL
	a.mu.RUnlock()

	allSelected := hasOverride && chLevel == store.NotifyAll
	mentionsSelected := hasOverride && chLevel == store.NotifyMentions
	mutedSelected := hasOverride && chLevel == store.NotifyNothing
	defaultSelected := !hasOverride

	items := []ContextMenuItem{
		{Label: "Notifications", Children: []ContextMenuItem{
			{Label: "All messages", Selected: allSelected, Action: func() {
				a.mu.Lock()
				conn.ChannelNotify[channelID] = store.NotifyAll
				a.mu.Unlock()
				go store.UpdateChannelNotifyLevel(a.PublicKey, serverURL, channelID, store.NotifyAll)
			}},
			{Label: "Only @mentions", Selected: mentionsSelected, Action: func() {
				a.mu.Lock()
				conn.ChannelNotify[channelID] = store.NotifyMentions
				a.mu.Unlock()
				go store.UpdateChannelNotifyLevel(a.PublicKey, serverURL, channelID, store.NotifyMentions)
			}},
			{Label: "Muted", Selected: mutedSelected, Action: func() {
				a.mu.Lock()
				conn.ChannelNotify[channelID] = store.NotifyNothing
				a.mu.Unlock()
				go store.UpdateChannelNotifyLevel(a.PublicKey, serverURL, channelID, store.NotifyNothing)
			}},
			{IsSep: true},
			{Label: "Use server default", Selected: defaultSelected, Action: func() {
				a.mu.Lock()
				delete(conn.ChannelNotify, channelID)
				a.mu.Unlock()
				go store.DeleteChannelNotifyLevel(a.PublicKey, serverURL, channelID)
			}},
		}},
	}

	a.ContextMenu.Show(x, y, "Channel Settings", items)
}
