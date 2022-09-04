package cfsync

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Notifiarr/notifiarr/pkg/apps"
	"github.com/Notifiarr/notifiarr/pkg/triggers/common"
	"github.com/Notifiarr/notifiarr/pkg/website"
	"golift.io/starr"
	"golift.io/starr/sonarr"
)

const TrigRPSyncSonarr common.TriggerName = "Starting Sonarr Release Profile TRaSH sync."

// SonarrTrashPayload is the payload sent and received
// to/from notifarr.com when updating custom formats for Sonarr.
type SonarrTrashPayload struct {
	Instance           int                         `json:"instance"`
	Name               string                      `json:"name"`
	ReleaseProfiles    []*sonarr.ReleaseProfile    `json:"releaseProfiles,omitempty"`
	QualityProfiles    []*sonarr.QualityProfile    `json:"qualityProfiles,omitempty"`
	CustomFormats      []*sonarr.CustomFormat      `json:"customFormats,omitempty"`
	QualityDefinitions []*sonarr.QualityDefinition `json:"qualityDefinitions,omitempty"`
	Error              string                      `json:"error"`
	NewMaps            *cfMapIDpayload             `json:"newMaps,omitempty"`
}

// SyncSonarrRP initializes a release profile sync with sonarr.
func (a *Action) SyncSonarrRP(event website.EventType) {
	a.cmd.Exec(event, TrigRPSyncSonarr)
}

// syncSonarr triggers a custom format sync for Sonarr.
func (c *cmd) syncSonarr(event website.EventType) {
	if c.ClientInfo == nil || len(c.ClientInfo.Actions.Sync.SonarrInstances) < 1 {
		c.Debugf("Cannot sync Sonarr Release Profiles. Website provided 0 instances.")
		return
	} else if len(c.Apps.Sonarr) < 1 {
		c.Debugf("Cannot sync Sonarr Release Profiles. No Sonarr instances configured.")
		return
	}

	for i, app := range c.Apps.Sonarr {
		instance := i + 1
		if app.URL == "" || app.APIKey == "" || app.Timeout.Duration < 0 ||
			!c.ClientInfo.Actions.Sync.SonarrInstances.Has(instance) {
			c.Debugf("CF Sync Skipping Sonarr instance %d. Not in sync list: %v",
				instance, c.ClientInfo.Actions.Sync.SonarrInstances)
			continue
		}

		if err := c.syncSonarrRP(instance, app); err != nil {
			c.Errorf("[%s requested] Sonarr Release Profiles sync for '%d:%s' failed: %v", event, instance, app.URL, err)
			continue
		}

		c.Printf("[%s requested] Synced Sonarr Release Profiles from Notifiarr: %d:%s", event, instance, app.URL)
	}
}

func (c *cmd) syncSonarrRP(instance int, app *apps.SonarrConfig) error {
	var (
		err     error
		payload = SonarrTrashPayload{Instance: instance, Name: app.Name, NewMaps: c.sonarrRP[instance]}
	)

	payload.QualityProfiles, err = app.GetQualityProfiles()
	if err != nil {
		return fmt.Errorf("getting quality profiles: %w", err)
	}

	payload.ReleaseProfiles, err = app.GetReleaseProfiles()
	if err != nil {
		return fmt.Errorf("getting release profiles: %w", err)
	}

	payload.QualityDefinitions, err = app.GetQualityDefinitions()
	if err != nil {
		return fmt.Errorf("getting quality definitions: %w", err)
	}

	payload.CustomFormats, err = app.GetCustomFormats()
	if err != nil && !errors.Is(err, starr.ErrInvalidStatusCode) {
		return fmt.Errorf("getting custom formats: %w", err)
	}

	body, err := c.GetData(&website.Request{
		Route:   website.CFSyncRoute,
		Params:  []string{"app=sonarr"},
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("sending current profiles: %w", err)
	}

	delete(c.sonarrRP, instance)

	if body.Result != success {
		return fmt.Errorf("%w: %s", website.ErrInvalidResponse, body.Result)
	}

	if err := c.updateSonarrRP(instance, app, body.Details.Response); err != nil {
		return fmt.Errorf("updating application: %w", err)
	}

	return nil
}

func (c *cmd) updateSonarrRP(instance int, app *apps.SonarrConfig, data []byte) error {
	reply := &SonarrTrashPayload{}
	if err := json.Unmarshal(data, &reply); err != nil {
		return fmt.Errorf("bad json response: %w", err)
	}

	c.Printf("Received %d quality profiles and %d release profiles for Sonarr: %d:%s",
		len(reply.QualityProfiles), len(reply.ReleaseProfiles), instance, app.URL)

	maps := &cfMapIDpayload{
		QP:       []idMap{},
		RP:       []idMap{},
		CF:       []idMap{},
		Instance: instance,
		QPerr:    make(map[int64][]string),
		RPerr:    make(map[int64][]string),
		CFerr:    make(map[int][]string),
	}

	c.updateSonarrCustomFormats(app, reply, maps)
	c.updateSonarrReleaseProfiles(app, reply, maps)
	c.updateSonarrQualityProfiles(app, reply, maps)

	return c.postbackSonarrRP(instance, maps)
}

func (c *cmd) updateSonarrCustomFormats(app *apps.SonarrConfig, reply *SonarrTrashPayload, maps *cfMapIDpayload) {
	for idx, profile := range reply.CustomFormats {
		newID, existingID := profile.ID, profile.ID

		if _, err := app.UpdateCustomFormat(profile, existingID); err != nil {
			maps.CFerr[existingID] = append(maps.CFerr[existingID], err.Error())
			profile.ID = 0

			c.Debugf("Error Updating custom format [%d/%d] (attempting to ADD %d): %v",
				idx+1, len(reply.CustomFormats), existingID, err)

			newAdd, err2 := app.AddCustomFormat(profile)
			if err2 != nil {
				maps.CFerr[existingID] = append(maps.CFerr[existingID], err2.Error())
				c.ErrorfNoShare("Ensuring custom format [%d/%d] %d: (update) %v, (add) %v",
					idx+1, len(reply.CustomFormats), existingID, err, err2)

				continue
			}

			newID = newAdd.ID
		}

		maps.CF = append(maps.CF, idMap{profile.Name, int64(existingID), int64(newID)})
	}
}

func (c *cmd) updateSonarrReleaseProfiles(app *apps.SonarrConfig, reply *SonarrTrashPayload, maps *cfMapIDpayload) {
	for idx, profile := range reply.ReleaseProfiles {
		newID, existingID := profile.ID, profile.ID

		if _, err := app.UpdateReleaseProfile(profile); err != nil {
			maps.RPerr[existingID] = append(maps.RPerr[existingID], err.Error())

			profile.ID = 0

			c.Debugf("Error Updating release profile [%d/%d] (attempting to ADD %d): %v",
				idx+1, len(reply.ReleaseProfiles), existingID, err)

			newProfile, err2 := app.AddReleaseProfile(profile)
			if err2 != nil {
				maps.RPerr[existingID] = append(maps.RPerr[existingID], err2.Error())
				c.Errorf("Ensuring release profile [%d/%d] %d: (update) %v, (add) %v",
					idx+1, len(reply.ReleaseProfiles), existingID, err, err2)

				continue
			}

			newID = newProfile.ID
		}

		maps.RP = append(maps.RP, idMap{profile.Name, existingID, newID})
	}
}

func (c *cmd) updateSonarrQualityProfiles(app *apps.SonarrConfig, reply *SonarrTrashPayload, maps *cfMapIDpayload) {
	for idx, profile := range reply.QualityProfiles {
		newID, existingID := profile.ID, profile.ID

		if _, err := app.UpdateQualityProfile(profile); err != nil {
			maps.QPerr[existingID] = append(maps.QPerr[existingID], err.Error())
			profile.ID = 0

			c.Debugf("Error Updating quality format [%d/%d] (attempting to ADD %d): %v",
				idx+1, len(reply.QualityProfiles), existingID, err)

			newProfile, err2 := app.AddQualityProfile(profile)
			if err2 != nil {
				maps.QPerr[existingID] = append(maps.QPerr[existingID], err2.Error())
				c.Errorf("Ensuring quality format [%d/%d] %d: (update) %v, (add) %v",
					idx+1, len(reply.QualityProfiles), existingID, err, err2)

				continue
			}

			newID = newProfile.ID
		}

		maps.QP = append(maps.QP, idMap{profile.Name, existingID, newID})
	}
}

// postbackSonarrRP sends the changes back to notifiarr.com.
func (c *cmd) postbackSonarrRP(instance int, maps *cfMapIDpayload) error {
	if len(maps.QP) < 1 && len(maps.RP) < 1 && len(maps.CF) < 1 {
		return nil
	}

	_, err := c.GetData(&website.Request{
		Route:      website.CFSyncRoute,
		Params:     []string{"app=sonarr", "updateIDs=true"},
		Payload:    &SonarrTrashPayload{Instance: instance, NewMaps: maps},
		LogPayload: true,
	})
	if err != nil {
		c.sonarrRP[instance] = maps
		return fmt.Errorf("updating quality release ID map: %w", err)
	}

	delete(c.sonarrRP, instance)

	return nil
}
