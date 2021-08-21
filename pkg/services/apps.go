package services

import (
	"golift.io/cnfg"
)

// collectApps turns app configs into service checks if they have a name.
func (c *Config) collectApps() []*Service { //nolint:funlen,cyclop
	svcs := []*Service{}

	for _, a := range c.Apps.Lidarr {
		if a.Interval.Duration == 0 {
			a.Interval.Duration = DefaultCheckInterval
		}

		if a.Name != "" {
			svcs = append(svcs, &Service{
				Name:     a.Name,
				Type:     CheckHTTP,
				Value:    a.URL + "/api/v1/system/status?apikey=" + a.APIKey,
				Expect:   "200",
				Timeout:  cnfg.Duration{Duration: a.Timeout.Duration},
				Interval: a.Interval,
			})
		}
	}

	for _, a := range c.Apps.Radarr {
		if a.Interval.Duration == 0 {
			a.Interval.Duration = DefaultCheckInterval
		}

		if a.Name != "" {
			svcs = append(svcs, &Service{
				Name:     a.Name,
				Type:     CheckHTTP,
				Value:    a.URL + "/api/v3/system/status?apikey=" + a.APIKey,
				Expect:   "200",
				Timeout:  cnfg.Duration{Duration: a.Timeout.Duration},
				Interval: a.Interval,
			})
		}
	}

	for _, a := range c.Apps.Readarr {
		if a.Interval.Duration == 0 {
			a.Interval.Duration = DefaultCheckInterval
		}

		if a.Name != "" {
			svcs = append(svcs, &Service{
				Name:     a.Name,
				Type:     CheckHTTP,
				Value:    a.URL + "/api/v1/system/status?apikey=" + a.APIKey,
				Expect:   "200",
				Timeout:  cnfg.Duration{Duration: a.Timeout.Duration},
				Interval: a.Interval,
			})
		}
	}

	for _, a := range c.Apps.Sonarr {
		if a.Interval.Duration == 0 {
			a.Interval.Duration = DefaultCheckInterval
		}

		if a.Name != "" {
			svcs = append(svcs, &Service{
				Name:     a.Name,
				Type:     CheckHTTP,
				Value:    a.URL + "/api/v3/system/status?apikey=" + a.APIKey,
				Expect:   "200",
				Timeout:  cnfg.Duration{Duration: a.Timeout.Duration},
				Interval: a.Interval,
			})
		}
	}

	for _, d := range c.Apps.Deluge {
		if d.Interval.Duration == 0 {
			d.Interval.Duration = DefaultCheckInterval
		}

		if d.Name != "" {
			svcs = append(svcs, &Service{
				Name:     d.Name,
				Type:     CheckHTTP,
				Value:    d.Config.URL,
				Expect:   "200",
				Timeout:  cnfg.Duration{Duration: d.Timeout.Duration},
				Interval: d.Interval,
			})
		}
	}

	for _, q := range c.Apps.Qbit {
		if q.Interval.Duration == 0 {
			q.Interval.Duration = DefaultCheckInterval
		}

		if q.Name != "" {
			svcs = append(svcs, &Service{
				Name:     q.Name,
				Type:     CheckHTTP,
				Value:    q.URL,
				Expect:   "200",
				Timeout:  cnfg.Duration{Duration: q.Timeout.Duration},
				Interval: q.Interval,
			})
		}
	}

	return svcs
}
