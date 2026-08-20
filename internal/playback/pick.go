package playback

import (
	"litepan/internal/domain"
)

type Action uint8

const (
	ActionRedirect Action = iota
	ActionStream
)

type Intent struct {
	ForceProxy bool
	FileName   string
	Inline     bool
	WebDAV     bool
}

func PickAction(mode domain.DownloadMode, link domain.DownloadInfo, intent Intent) Action {
	if intent.ForceProxy || link.ForceProxy {
		return ActionStream
	}
	switch mode {
	case domain.DownloadRedirect:
		return ActionRedirect
	default:
		return ActionStream
	}
}
