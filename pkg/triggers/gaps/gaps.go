package gaps

import (
	"fmt"
	"time"

	"github.com/Notifiarr/notifiarr/pkg/triggers/common"
	"github.com/Notifiarr/notifiarr/pkg/website"
	"golift.io/starr/radarr"
)

/* Gaps allows filling gaps in Radarr collections. */

const TrigCollectionGaps common.TriggerName = "Sending Radarr Collection Gaps."

// Action contains the exported methods for this package.
type Action struct {
	cmd *cmd
}

type cmd struct {
	*common.Config
}

// New configures the library.
func New(config *common.Config) *Action {
	return &Action{cmd: &cmd{Config: config}}
}

// Create initializes the library.
func (a *Action) Create() {
	a.cmd.create()
}

func (c *cmd) create() {
	ci := c.ClientInfo

	var ticker *time.Ticker

	if ci != nil && ci.Actions.Gaps.Interval.Duration > 0 && len(ci.Actions.Gaps.Instances) > 0 {
		ticker = time.NewTicker(ci.Actions.Gaps.Interval.Duration)
		c.Printf("==> Collection Gaps Timer Enabled, interval:%s", ci.Actions.Gaps.Interval)
	}

	c.Add(&common.Action{
		Name: TrigCollectionGaps,
		Fn:   c.sendGaps,
		C:    make(chan website.EventType, 1),
		T:    ticker,
	})
}

// Send radarr collection gaps to the website.
func (a *Action) Send(event website.EventType) {
	a.cmd.Exec(event, TrigCollectionGaps)
}

func (c *cmd) sendGaps(event website.EventType) {
	if c.ClientInfo == nil || len(c.ClientInfo.Actions.Gaps.Instances) == 0 || len(c.Apps.Radarr) == 0 {
		c.Errorf("[%s requested] Cannot send Radarr Collection Gaps: instances or configured Radarrs (%d) are zero.",
			event, len(c.Apps.Radarr))
		return
	}

	for idx, app := range c.Apps.Radarr {
		instance := idx + 1
		if app.URL == "" || app.APIKey == "" || !c.ClientInfo.Actions.Gaps.Instances.Has(instance) {
			continue
		}

		type radarrGapsPayload struct {
			Instance int             `json:"instance"`
			Name     string          `json:"name"`
			Movies   []*radarr.Movie `json:"movies"`
		}

		movies, err := app.GetMovie(0)
		if err != nil {
			c.Errorf("[%s requested] Radarr Collection Gaps (%d:%s) failed: getting movies: %v", event, instance, app.URL, err)
			continue
		}

		c.SendData(&website.Request{
			Route:      website.DashRoute,
			Event:      event,
			LogPayload: true,
			LogMsg:     fmt.Sprintf("Radarr Collection Gaps (%d:%s)", instance, app.URL),
			Payload:    &radarrGapsPayload{Movies: movies, Name: app.Name, Instance: instance},
		})
	}
}