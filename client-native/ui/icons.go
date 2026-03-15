package ui

import (
	"image"
	"image/color"
	"image/draw"
	"sync"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"golang.org/x/exp/shiny/iconvg"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

// NIcon is a custom icon type that bypasses Gio widget.Icon.
// widget.Icon uses f32color.NRGBAToLinearRGBA for colors,
// which causes double gamma correction (linearization + GPU sRGB decode)
// and icons become invisible on dark backgrounds.
// NIcon uses sRGB colors directly and has a multi-key cache.
type NIcon struct {
	src   []byte
	mu    sync.Mutex
	cache map[iconKey]paint.ImageOp
}

type iconKey struct {
	size  int
	color color.NRGBA
}

func newIcon(data []byte) *NIcon {
	// Verify that data is valid IconVG
	if _, err := iconvg.DecodeMetadata(data); err != nil {
		panic(err)
	}
	return &NIcon{
		src:   data,
		cache: make(map[iconKey]paint.ImageOp),
	}
}

func (ic *NIcon) imageOp(sz int, clr color.NRGBA) paint.ImageOp {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	key := iconKey{sz, clr}
	if op, ok := ic.cache[key]; ok {
		return op
	}

	m, _ := iconvg.DecodeMetadata(ic.src)
	dx, dy := m.ViewBox.AspectRatio()
	h := int(float32(sz) * dy / dx)
	if h == 0 {
		h = sz
	}

	img := image.NewRGBA(image.Rectangle{Max: image.Point{X: sz, Y: h}})
	var r iconvg.Rasterizer
	r.SetDstImage(img, img.Bounds(), draw.Src)
	// Color in sRGB — no linearization, GPU displays it correctly
	m.Palette[0] = color.RGBA{R: clr.R, G: clr.G, B: clr.B, A: clr.A}
	iconvg.Decode(&r, ic.src, &iconvg.DecodeOptions{Palette: &m.Palette})

	op := paint.NewImageOp(img)
	ic.cache[key] = op
	return op
}

var (
	IconMic        *NIcon
	IconMicOff     *NIcon
	IconVolumeUp   *NIcon
	IconVolumeOff  *NIcon
	IconCallEnd    *NIcon
	IconSettings   *NIcon
	IconClose      *NIcon
	IconEdit       *NIcon
	IconDelete     *NIcon
	IconAdd        *NIcon
	IconHome       *NIcon
	IconSend       *NIcon
	IconReply      *NIcon
	IconPin        *NIcon
	IconSearch     *NIcon
	IconPerson     *NIcon
	IconPersonAdd  *NIcon
	IconGroup      *NIcon
	IconBlock      *NIcon
	IconCopy       *NIcon
	IconCheck      *NIcon
	IconCancel     *NIcon
	IconRefresh    *NIcon
	IconMenu       *NIcon
	IconExpand     *NIcon
	IconCollapse   *NIcon
	IconChat       *NIcon
	IconEmoji      *NIcon
	IconUpload     *NIcon
	IconSave       *NIcon
	IconExit       *NIcon
	IconMoreVert   *NIcon
	IconArrowUp    *NIcon
	IconArrowDown  *NIcon
	IconHeadset    *NIcon
	IconDragHandle *NIcon
	IconMonitor    *NIcon
	IconBack       *NIcon
	IconFolder     *NIcon
	IconFile       *NIcon
	IconDownload   *NIcon
	IconPlayArrow  *NIcon
	IconPause      *NIcon
	IconVideocam   *NIcon
	IconStorage      *NIcon
	IconImage        *NIcon
	IconChevronRight *NIcon
	IconLink            *NIcon
	IconSchedule        *NIcon
	IconBookmark        *NIcon
	IconBookmarkBorder  *NIcon
	IconViewColumn      *NIcon
	IconLock            *NIcon
	IconNotifications   *NIcon
	IconRepeat          *NIcon
)

func init() {
	IconMic = newIcon(icons.AVMic)
	IconMicOff = newIcon(icons.AVMicOff)
	IconVolumeUp = newIcon(icons.AVVolumeUp)
	IconVolumeOff = newIcon(icons.AVVolumeOff)
	IconCallEnd = newIcon(icons.CommunicationCallEnd)
	IconSettings = newIcon(icons.ActionSettings)
	IconClose = newIcon(icons.NavigationClose)
	IconEdit = newIcon(icons.EditorModeEdit)
	IconDelete = newIcon(icons.ActionDelete)
	IconAdd = newIcon(icons.ContentAdd)
	IconHome = newIcon(icons.ActionHome)
	IconSend = newIcon(icons.ContentSend)
	IconReply = newIcon(icons.ContentReply)
	IconPin = newIcon(icons.ActionOfflinePin)
	IconSearch = newIcon(icons.ActionSearch)
	IconPerson = newIcon(icons.SocialPerson)
	IconPersonAdd = newIcon(icons.SocialPersonAdd)
	IconGroup = newIcon(icons.SocialGroup)
	IconBlock = newIcon(icons.ContentBlock)
	IconCopy = newIcon(icons.ContentContentCopy)
	IconCheck = newIcon(icons.NavigationCheck)
	IconCancel = newIcon(icons.NavigationCancel)
	IconRefresh = newIcon(icons.NavigationRefresh)
	IconMenu = newIcon(icons.NavigationMenu)
	IconExpand = newIcon(icons.NavigationExpandMore)
	IconCollapse = newIcon(icons.NavigationExpandLess)
	IconChat = newIcon(icons.CommunicationChat)
	IconEmoji = newIcon(icons.SocialMood)
	IconUpload = newIcon(icons.FileFileUpload)
	IconSave = newIcon(icons.ContentSave)
	IconExit = newIcon(icons.ActionExitToApp)
	IconMoreVert = newIcon(icons.NavigationMoreVert)
	IconArrowUp = newIcon(icons.NavigationArrowUpward)
	IconArrowDown = newIcon(icons.NavigationArrowDownward)
	IconHeadset = newIcon(icons.HardwareHeadset)
	IconDragHandle = newIcon(icons.EditorDragHandle)
	IconMonitor = newIcon(icons.HardwareDesktopWindows)
	IconBack = newIcon(icons.NavigationArrowBack)
	IconFolder = newIcon(icons.FileFolder)
	IconFile = newIcon(icons.EditorInsertDriveFile)
	IconDownload = newIcon(icons.FileFileDownload)
	IconPlayArrow = newIcon(icons.AVPlayArrow)
	IconPause = newIcon(icons.AVPause)
	IconVideocam = newIcon(icons.AVVideocam)
	IconStorage  = newIcon(icons.DeviceStorage)
	IconImage    = newIcon(icons.ImageImage)
	IconChevronRight = newIcon(icons.NavigationChevronRight)
	IconLink         = newIcon(icons.ContentLink)
	IconSchedule     = newIcon(icons.ActionSchedule)
	IconBookmark       = newIcon(icons.ActionBookmark)
	IconBookmarkBorder = newIcon(icons.ActionBookmarkBorder)
	IconViewColumn     = newIcon(icons.ActionViewColumn)
	IconLock           = newIcon(icons.ActionLock)
	IconNotifications  = newIcon(icons.SocialNotifications)
	IconRepeat         = newIcon(icons.AVRepeat)
}

// layoutIcon renders an icon at the given size and color.
func layoutIcon(gtx layout.Context, icon *NIcon, sizeDp unit.Dp, clr color.NRGBA) layout.Dimensions {
	sz := gtx.Dp(sizeDp)
	if sz == 0 {
		sz = 24
	}
	size := image.Pt(sz, sz)

	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()

	op := icon.imageOp(sz, clr)
	op.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	return layout.Dimensions{Size: size}
}
