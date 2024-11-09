// Lophiid distributed honeypot
// Copyright (C) 2024 Niels Heinen
//
// This program is free software; you can redistribute it and/or modify it
// under the terms of the GNU General Public License as published by the
// Free Software Foundation; either version 2 of the License, or (at your
// option) any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY
// or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License
// for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, write to the Free Software Foundation, Inc.,
// 59 Temple Place, Suite 330, Boston, MA 02111-1307 USA
package models

import (
	"sync"
	"time"
)

type Session struct {
	ID             int64  `ksql:"id,skipInserts" json:"id" doc:"Database ID for the session"`
	Active         bool   `ksql:"active" json:"active" doc:"Is the session active"`
	IP             string `ksql:"ip" json:"ip" doc:"IP of the client"`
	LastRuleServed ContentRule
	RuleIDsServed  map[int64]int64
	CreatedAt      time.Time `ksql:"created_at,skipInserts,skipUpdates" json:"created_at" doc:"Creation date of the session in the database (not session start!)"`
	UpdatedAt      time.Time `ksql:"updated_at,timeNowUTC" json:"updated_at" doc:"Date and time of last update"`
	StartedAt      time.Time `ksql:"started_at" json:"started_at" doc:"Start time of the session"`
	EndedAt        time.Time `ksql:"ended_at" json:"ended_at" doc:"End time of the session"`
	Mu             sync.RWMutex
}

func (c *Session) ModelID() int64 { return c.ID }

// HasServedRule checks if the session has served the given rule.
func (c *Session) HasServedRule(ruleID int64) bool {
	c.Mu.RLock()
	defer c.Mu.RUnlock()
	_, ok := c.RuleIDsServed[ruleID]
	return ok
}

// ServedRuleWithContent updates the session with the given rule and content ID.
func (c *Session) ServedRuleWithContent(ruleID int64, contentID int64) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.RuleIDsServed[ruleID] = contentID
}

// NewSession creates a new session.
func NewSession() *Session {
	return &Session{
		RuleIDsServed: make(map[int64]int64),
	}
}
