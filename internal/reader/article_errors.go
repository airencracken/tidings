package reader

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

var (
	errArticleType     = errors.New("publisher did not return an article page")
	errArticleTooLarge = errors.New("article exceeds the size limit")
	errArticleEmpty    = errors.New("no readable story found")
	errArticleParse    = errors.New("article could not be extracted")
)

// Return only controlled text and structured log fields. Raw network errors may
// contain article URLs, signed query values, or publisher-supplied text.
func articleFailure(err error) (message, reason string, status int) {
	var publisher *publisherError
	if errors.As(err, &publisher) {
		status = publisher.Status
		reason = "publisher-status"
		switch status {
		case http.StatusUnauthorized:
			return "The publisher requires sign-in for this article (HTTP 401).", reason, status
		case http.StatusForbidden:
			return "The publisher refused this fetch (HTTP 403).", reason, status
		case http.StatusNotFound:
			return "The publisher could not find this article (HTTP 404).", reason, status
		case http.StatusTooManyRequests:
			return "The publisher is limiting requests (HTTP 429). Try again later.", reason, status
		default:
			return fmt.Sprintf("The publisher returned HTTP %d. Try again later.", status), reason, status
		}
	}
	var network net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout():
		return "The publisher took too long to respond. Try again later.", "timeout", 0
	case errors.Is(err, errArticleType):
		return "The publisher returned a file instead of an article page.", "content-type", 0
	case errors.Is(err, errArticleTooLarge):
		return "This article is too large to fetch here.", "size-limit", 0
	case errors.Is(err, errArticleEmpty), errors.Is(err, errArticleParse):
		return "Could not find the story text on this page.", "extraction", 0
	default:
		return "Could not connect to the publisher. Try again later.", "connection", 0
	}
}
