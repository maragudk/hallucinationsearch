package model

type JobName string

func (j JobName) String() string {
	return string(j)
}

const (
	JobNameGenerateResults   JobName = "generate-results"
	JobNameGenerateResult    JobName = "generate-result"
	JobNameGenerateWebsite   JobName = "generate-website"
	JobNameGenerateAds       JobName = "generate-ads"
	JobNameGenerateAd        JobName = "generate-ad"
	JobNameGenerateAdWebsite JobName = "generate-ad-website"
)

// GenerateResultsJobData is the payload for the [JobNameGenerateResults] job, which
// fans out one [JobNameGenerateResult] job per missing position.
type GenerateResultsJobData struct {
	QueryID QueryID
}

// GenerateResultJobData is the payload for the [JobNameGenerateResult] job, which
// fabricates a single result at the given position for the given query.
type GenerateResultJobData struct {
	QueryID  QueryID
	Position int
}

// GenerateWebsiteJobData is the payload for the [JobNameGenerateWebsite] job, which
// fabricates a full HTML document for a single result.
type GenerateWebsiteJobData struct {
	ResultID ResultID
}

// GenerateAdsJobData is the payload for the [JobNameGenerateAds] job, which
// fans out one [JobNameGenerateAd] job per missing ad position.
type GenerateAdsJobData struct {
	QueryID QueryID
}

// GenerateAdJobData is the payload for the [JobNameGenerateAd] job, which
// fabricates a single ad at the given position for the given query.
type GenerateAdJobData struct {
	QueryID  QueryID
	Position int
}

// GenerateAdWebsiteJobData is the payload for the [JobNameGenerateAdWebsite] job, which
// fabricates a full HTML document for a single ad.
type GenerateAdWebsiteJobData struct {
	AdID AdID
}
