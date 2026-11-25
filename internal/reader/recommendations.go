package reader

import (
	"context"
	"time"
)

const (
	recommendationLimit = 12
	recommendationDays  = 30
	feedbackDays        = 90
)

func (s *Store) recommendations(ctx context.Context, uid int64, filter Filter, now time.Time) ([]Entry, error) {
	where, filterArgs := entryWhere(uid, filter)
	// Feedback belongs to this reader and only comes from sources they can
	// still read. Marking an entire page read carries no preference signal.
	// Shared tags give a smaller boost to related sources. Later stories from
	// the same source are penalized to give quieter publishers a chance.
	query := `
WITH feedback AS (
  SELECT e.feed_id,
         max(-12, min(12, sum(CASE WHEN st.skipped THEN -3 ELSE 4 END))) AS affinity
  FROM states st JOIN entries e ON e.id=st.entry_id
  WHERE st.user_id=? AND e.published>=? AND (st.saved=1 OR st.skipped=1)
    AND ` + visible + `
  GROUP BY e.feed_id
), source_tags AS (
  SELECT feed_id, tag FROM subscription_tags WHERE user_id=?
  UNION
  SELECT feed_id, tag FROM household_tags WHERE EXISTS(SELECT 1 FROM users WHERE id=? AND group_optin=1)
), related AS (
  SELECT candidate.feed_id, max(feedback.affinity) AS affinity
  FROM feedback
  JOIN source_tags liked ON liked.feed_id=feedback.feed_id
  JOIN source_tags candidate ON candidate.tag=liked.tag AND candidate.feed_id<>liked.feed_id
  WHERE feedback.affinity>0
  GROUP BY candidate.feed_id
), candidates AS (
  SELECT e.id, e.feed_id, e.published, e.url,
         row_number() OVER (PARTITION BY e.feed_id ORDER BY e.published DESC, e.id DESC) AS source_rank
  FROM entries e LEFT JOIN states st ON st.entry_id=e.id AND st.user_id=?
  WHERE e.published BETWEEN ? AND ? AND coalesce(st.saved,0)=0 AND ` + where + `
), scored AS (
  SELECT c.id, c.url, c.published,
         coalesce(f.affinity,0) + coalesce(r.affinity,0)/2
           - (? - c.published)/172800 - 9*(c.source_rank-1) AS score,
         CASE WHEN f.affinity > 0 THEN 'From a source you save'
              WHEN r.affinity >= 2 THEN 'Shares a tag with a source you save'
              ELSE '' END AS reason
  FROM candidates c
  LEFT JOIN feedback f ON f.feed_id=c.feed_id
  LEFT JOIN related r ON r.feed_id=c.feed_id
), unique_stories AS (
  SELECT id, published, score, reason,
         row_number() OVER (PARTITION BY url ORDER BY score DESC, published DESC, id DESC) AS copy_rank
  FROM scored
), ranked AS (
  SELECT id, score, reason FROM unique_stories WHERE copy_rank=1
  ORDER BY score DESC, published DESC, id DESC
  LIMIT ?
)
SELECT e.id, e.feed_id, e.title, e.url,
       CASE WHEN length(e.body)>240 THEN substr(e.body,1,240)||'…' ELSE e.body END,
       e.image_url, e.published, f.title,
       coalesce((SELECT t.tag FROM subscription_tags t WHERE t.user_id=? AND t.feed_id=e.feed_id ORDER BY t.position,t.tag LIMIT 1),
                (SELECT t.tag FROM household_tags t WHERE t.feed_id=e.feed_id
                   AND EXISTS(SELECT 1 FROM users WHERE id=? AND group_optin=1) ORDER BY t.position,t.tag LIMIT 1),''),
       ranked.reason
FROM ranked JOIN entries e ON e.id=ranked.id JOIN feeds f ON f.id=e.feed_id
ORDER BY ranked.score DESC, e.published DESC, e.id DESC`

	args := []any{uid, now.AddDate(0, 0, -feedbackDays).Unix(), uid, uid, uid, uid,
		uid, now.AddDate(0, 0, -recommendationDays).Unix(), now.Unix()}
	args = append(args, filterArgs...)
	args = append(args, now.Unix(), recommendationLimit, uid, uid)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.FeedID, &e.Title, &e.URL, &e.Body, &e.ImageURL, &e.Published, &e.Source, &e.Category, &e.RecommendationReason); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
