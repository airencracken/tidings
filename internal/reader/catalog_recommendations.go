package reader

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func (s *Store) catalogFeedback(ctx context.Context, uid int64, now time.Time) (map[int64]int, error) {
	rows, err := s.DB.QueryContext(ctx, `
SELECT e.feed_id, max(-3, min(6, sum(CASE WHEN st.skipped THEN -1 ELSE 2 END)))
FROM states st
JOIN entries e ON e.id=st.entry_id
JOIN subscriptions sub ON sub.feed_id=e.feed_id AND sub.user_id=st.user_id
WHERE st.user_id=? AND (e.import_owner IS NULL OR e.import_owner=st.user_id)
  AND (st.saved=1 OR st.skipped=1) AND e.published BETWEEN ? AND ?
GROUP BY e.feed_id`, uid, now.AddDate(0, 0, -feedbackDays).Unix(), now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int64]int{}
	for rows.Next() {
		var id int64
		var weight int
		if err := rows.Scan(&id, &weight); err != nil {
			return nil, err
		}
		result[id] = weight
	}
	return result, rows.Err()
}

type catalogInterest struct {
	Weight int
	Reason string
}

type catalogPick struct {
	Source catalogSource
	Score  int
}

func recommendCatalog(catalog []Suggestion, enabled map[string]bool, followed []Feed, feedback map[int64]int) []catalogSource {
	publishers := map[string]Suggestion{}
	for _, item := range catalog {
		publishers[item.URL] = item
	}
	followedURLs := map[string]bool{}
	languages := map[string]bool{"English": true}
	interests := map[string]catalogInterest{}
	addInterest := func(topic, reason string, weight int) {
		interest := interests[topic]
		interest.Weight += weight
		if interest.Reason == "" {
			interest.Reason = reason
		}
		interests[topic] = interest
	}
	for _, feed := range followed {
		followedURLs[feed.URL] = true
		item, inCatalog := publishers[feed.URL]
		if inCatalog {
			languages[item.Language] = true
			addInterest(catalogWords(item.Category), "Because you follow "+item.Title, 3+feedback[feed.ID])
		}
		for _, tag := range feed.Tags {
			topic := catalogWords(tag)
			if topic != "" && (!inCatalog || topic != catalogWords(item.Category)) {
				addInterest(topic, "Matches your tag "+tag, 2+feedback[feed.ID])
			}
		}
	}

	var picks []catalogPick
	for _, item := range catalog {
		if followedURLs[item.URL] || !enabled[item.Category] || !languages[item.Language] {
			continue
		}
		interest := interests[catalogWords(item.Category)]
		score, reason := interest.Weight*3, interest.Reason
		text := " " + catalogWords(item.Title+" "+item.Description) + " "
		for topic, related := range interests {
			if utf8.RuneCountInString(topic) >= 3 && related.Weight > 0 && strings.Contains(text, " "+topic+" ") &&
				(related.Weight > score || (related.Weight == score && related.Reason < reason)) {
				score, reason = related.Weight, related.Reason
			}
		}
		if score > 0 {
			picks = append(picks, catalogPick{Source: catalogSource{Suggestion: item, Reason: reason}, Score: score})
		}
	}
	sort.SliceStable(picks, func(i, j int) bool { return picks[i].Score > picks[j].Score })
	var result []catalogSource
	counts := map[string]int{}
	for _, pick := range picks {
		if counts[pick.Source.Category] < 2 {
			result = append(result, pick.Source)
			counts[pick.Source.Category]++
		}
		if len(result) == 6 {
			break
		}
	}
	return result
}

func catalogWords(text string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}), " ")
}
