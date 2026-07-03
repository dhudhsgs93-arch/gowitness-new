package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/sensepost/gowitness/pkg/log"
	"github.com/sensepost/gowitness/pkg/models"
	"gorm.io/gorm/clause"
)

// autoRule is a heuristic that maps a result to a review status. A rule matches
// when any of its (non-nil) matchers hit the corresponding haystack.
type autoRule struct {
	status string
	reason string
	title  *regexp.Regexp
	body   *regexp.Regexp
	tech   *regexp.Regexp
	code   func(int) bool // optional: match on the HTTP response code
}

func ci(pattern string) *regexp.Regexp { return regexp.MustCompile("(?i)" + pattern) }

// autoRules are evaluated top-to-bottom; the FIRST match wins, so the strongest
// signals (attention) precede weaker ones (interesting) and finally the junk
// rules. Junk rules are kept strict (title / very specific phrases) so real
// results are never hidden by an over-broad match.
var autoRules = []autoRule{
	// ---- attention: look now ----
	{status: "attention", reason: "directory listing", title: ci(`index of /|directory listing`), body: ci(`<title>index of /`)},
	{status: "attention", reason: "stack trace / debug", body: ci(`whitelabel error page|traceback \(most recent call last\)|werkzeug|fatal error:|sqlstate\[|stack trace:|uncaught exception|debug\s*=\s*true`)},
	{status: "attention", reason: "setup / installer", title: ci(`setup wizard|installation|install wordpress|configuration wizard|initial setup`)},
	{status: "attention", reason: "unauth-prone infra", body: ci(`spring boot|/actuator|consul by hashicorp|kubernetes dashboard|prometheus time series|phpmyadmin|adminer|minio console`), tech: ci(`actuator|consul|kubernetes|prometheus|phpmyadmin|adminer|minio`)},

	// ---- fuzz: alive-but-empty hosts → content-discovery candidates (feed to ffuf) ----
	{status: "fuzz", reason: "404 not found", code: func(c int) bool { return c == 404 }},
	{status: "fuzz", reason: "soft 404 / not found", title: ci(`\b404\b|not found|page not found`)},
	{status: "fuzz", reason: "default server page", title: ci(`welcome to nginx|apache2? (ubuntu|debian|centos)? ?default page|iis windows|^it works!?$|test page for the (apache|nginx)|welcome to centos|default web site page`)},

	// ---- interesting: worth a look ----
	{status: "interesting", reason: "login / auth", title: ci(`\blog ?in\b|\bsign ?in\b|authentication required`), body: ci(`type=["']password["']|name=["']password["']`)},
	{status: "interesting", reason: "admin / dashboard", title: ci(`\badmin|\bdashboard|control panel|cpanel|webmail|management console`)},
	{status: "interesting", reason: "known app / panel", body: ci(`grafana|kibana|jenkins|gitlab|gitea|jira|confluence|portainer|rabbitmq|argo cd|harbor`), tech: ci(`grafana|kibana|jenkins|gitlab|jira|confluence|portainer`)},
	{status: "interesting", reason: "api docs", title: ci(`swagger ui|graphiql|api documentation|redoc`), body: ci(`swagger-ui|graphiql|openapi|swagger\.json`)},

	// ---- junk: hide (strict) ----
	{status: "junk", reason: "cdn/waf block", title: ci(`attention required|access denied|request blocked|just a moment|sucuri website firewall|error 10\d\d`)},
	{status: "junk", reason: "parked / placeholder", title: ci(`domain is for sale|parked|buy this domain|under construction|coming soon|site maintenance|account suspended`)},
}

// classifyResult returns the (status, reason) of the first matching rule, or
// empty strings when nothing matches.
func classifyResult(title, body, tech string, code int) (string, string) {
	for _, rule := range autoRules {
		if (rule.title != nil && rule.title.MatchString(title)) ||
			(rule.body != nil && rule.body.MatchString(body)) ||
			(rule.tech != nil && rule.tech.MatchString(tech)) ||
			(rule.code != nil && rule.code(code)) {
			return rule.status, rule.reason
		}
	}
	return "", ""
}

type autoTagRequest struct {
	// Overwrite re-tags results that already have a review. Default false only
	// touches unreviewed results, so manual triage is never clobbered.
	Overwrite bool `json:"overwrite"`
}

type autoTagResponse struct {
	Scanned int64            `json:"scanned"`
	Tagged  int64            `json:"tagged"`
	Counts  map[string]int64 `json:"counts"`
}

// AutoTagHandler applies the heuristic rules over stored results and writes the
// matched review status (with an "auto: <reason>" comment on newly-created
// reviews). Existing reviews are preserved unless Overwrite is set, and an
// existing comment is never overwritten.
//
//	@Summary		Auto-triage
//	@Description	Heuristically tag results (interesting/attention/junk/fuzz) by title, body, tech and status code.
//	@Tags			Reviews
//	@Accept			json
//	@Produce		json
//	@Param			body	body		autoTagRequest	false	"Options"
//	@Success		200		{object}	autoTagResponse
//	@Router			/review/auto-tag [post]
func (h *ApiHandler) AutoTagHandler(w http.ResponseWriter, r *http.Request) {
	var req autoTagRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body is optional

	// Build the skip-set. Default: skip every already-reviewed result (only
	// unseen get tagged). Overwrite: skip only MANUAL reviews (comment not
	// prefixed "auto:"), so previous auto-tags are refreshed while hand triage
	// is never clobbered.
	skip := map[uint]struct{}{}
	{
		type rev struct {
			ResultID uint
			Comment  string
		}
		var revs []rev
		h.DB.Model(&models.Review{}).Select("result_id, comment").Find(&revs)
		for _, rv := range revs {
			if req.Overwrite && strings.HasPrefix(rv.Comment, "auto:") {
				continue // refresh auto-tags in overwrite mode
			}
			skip[rv.ResultID] = struct{}{}
		}
	}

	type row struct {
		ID           uint
		Title        string
		HTML         string
		ResponseCode int
		Failed       bool
	}

	counts := map[string]int64{}
	var scanned, tagged int64
	const batch = 300
	var lastID uint
	for {
		var rows []row
		if err := h.DB.Model(&models.Result{}).
			Select("id, title, html, response_code, failed").
			Where("id > ?", lastID).
			Order("id").Limit(batch).Find(&rows).Error; err != nil {
			log.Error("auto-tag scan failed", "err", err)
			http.Error(w, `{"error":"db error"}`, http.StatusInternalServerError)
			return
		}
		if len(rows) == 0 {
			break
		}

		// technologies for this batch, grouped by result id
		ids := make([]uint, len(rows))
		for i, rw := range rows {
			ids[i] = rw.ID
			lastID = rw.ID
		}
		type techRow struct {
			ResultID uint
			Value    string
		}
		var techs []techRow
		h.DB.Model(&models.Technology{}).Select("result_id, value").
			Where("result_id IN ?", ids).Find(&techs)
		techByID := make(map[uint][]string)
		for _, t := range techs {
			techByID[t.ResultID] = append(techByID[t.ResultID], t.Value)
		}

		for _, rw := range rows {
			scanned++
			if rw.Failed {
				continue
			}
			if _, seen := skip[rw.ID]; seen {
				continue
			}
			status, reason := classifyResult(rw.Title, rw.HTML, strings.Join(techByID[rw.ID], " "), rw.ResponseCode)
			if status == "" {
				continue
			}
			review := models.Review{
				ResultID:  rw.ID,
				Status:    status,
				Comment:   "auto: " + reason,
				UpdatedAt: time.Now(),
			}
			// On conflict, refresh status/comment/updated_at. This is only
			// reached for unseen results (new insert) or prior AUTO tags in
			// overwrite mode — manual reviews were excluded from the skip-set
			// above, so a hand-written comment is never touched here.
			if err := h.DB.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "result_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"status", "comment", "updated_at"}),
			}).Create(&review).Error; err != nil {
				log.Error("auto-tag upsert failed", "id", rw.ID, "err", err)
				continue
			}
			counts[status]++
			tagged++
		}
	}

	log.Info("auto-tag complete", "scanned", scanned, "tagged", tagged)
	json.NewEncoder(w).Encode(autoTagResponse{Scanned: scanned, Tagged: tagged, Counts: counts})
}
