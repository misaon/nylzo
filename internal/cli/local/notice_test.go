package local

import (
	"testing"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/localclient"
)

func TestANoticeThatFindsTheQueueFullIsDroppedWithoutBlocking(t *testing.T) {
	t.Parallel()
	current := &session{environment: &command.Environment{}, notices: make(chan localclient.Notice, 1)}
	notify := current.config().Notify
	notify(localclient.HandoffTaken)
	notify(localclient.HandoffRefused)
	if queued := <-current.notices; queued != localclient.HandoffTaken || len(current.notices) != 0 {
		t.Fatalf("the queue holds %v and %d more", queued, len(current.notices))
	}
}

func TestAnUnknownNoticeChangesNothing(t *testing.T) {
	t.Parallel()
	current := &session{environment: &command.Environment{}}
	current.report(localclient.Notice(0))
	if current.taken {
		t.Fatal("an unknown notice marked the link as taken")
	}
}
