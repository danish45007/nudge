package activity

import (
	"github.com/knadh/koanf/v2"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	prp "nudge/internal/database/pr"
	"nudge/internal/database/repository"
	"time"
)

type Activity struct {
	ko *koanf.Koanf
	db *mongo.Database
	lo *log.Logger
}

func Init(ko *koanf.Koanf, db *mongo.Database, lo *log.Logger) *Activity {
	return &Activity{
		ko: ko,
		db: db,
		lo: lo,
	}
}

type ActivityDetection struct {
	Detected bool
}

type DelayedPRDetails struct {
	Repository repository.RepoModel
	DelayedPR  prp.PRModel
}

type delayedPRChanDetails struct {
	Repository    repository.RepoModel
	DelayedPRList chan []prp.PRModel
}

func (activity *Activity) ActivityCheckTrigger() (*[]DelayedPRDetails, error) {
	repo := repository.Init(activity.db)
	repoList, repoFetchErr := repo.GetAll()
	if repoFetchErr != nil {
		activity.lo.Printf("Failed to fetch repositories for activity detection %v", repoFetchErr)
		return nil, repoFetchErr
	}

	delayedPRChanList := make([]delayedPRChanDetails, 0)
	for _, repo := range *repoList {
		delayedPRChanList = append(delayedPRChanList, delayedPRChanDetails{
			Repository:    repo,
			DelayedPRList: activity.findDelayedPRs(repo),
		})
	}

	delayedPrs := make([]DelayedPRDetails, 0)
	for _, prs := range delayedPRChanList {
		for _, pr := range <-prs.DelayedPRList {
			delayedPrs = append(delayedPrs, DelayedPRDetails{
				Repository: prs.Repository,
				DelayedPR:  pr,
			})
		}
	}

	return &delayedPrs, nil
}

// checkForActivity Once a pull request’s actual lifetime crosses the estimated lifetime
// (using the effort estimation models), the next module, Activity Detection, is run,
// which checks for any activity in the pull request environment. If there is an activity
// observed in the last 24 hours, then the workflow is terminated.
func (activity *Activity) checkForActivity(prModel prp.PRModel) *ActivityDetection {

	//  Activity Detection
	activityDetection := new(ActivityDetection)
	/**
	Pull request state changes

	A state change in a pull request strongly indicates that one of the actors
	(author or reviewer) has been acted on the pull request recently.
	*/

	/**
	Comments

	Once a pull request is submitted for review, reviewers can add comments
	to recommend changes or seek clarification on a specific code change.
	Authors of the pull request can also reply to the comment thread that
	is started by the reviewers if they have any follow-up questions. In
	addition to placing the comments and replying to them, the actors can
	also change the status of the comments. Typical statuses are “Active,”
	which means the comment has just been placed; “Resolved,” which means
	the comment has been resolved by the author of the pull request by making
	the changes prescribed by the reviewers; “Won’t fix,” which means the
	author would like to discard the review recommendation without addressing it;
	and “Closed,” which means the comment thread is going to be closed,
	as there are no more follow-up action items or discussions needed.
	*/

	/**
	Updates

	After a pull request has been created, authors can keep pushing new updates
	in the form of commits. These commits are changes that authors are making in
	response to review recommendations or improvements the authors themselves
	decided to push into the pull request. Under some special circumstances,
	someone other than the author or the reviewer can also push new updates
	into a pull request but that is a rare occurrence. New updates or iterations
	are a very strong indicator that the author is making progress on the pull request.
	*/
	now := time.Now()
	workflowLastUpdated := time.Unix(*prModel.WorkflowLastActivity, 0)
	hoursSinceLastActivity := now.Sub(workflowLastUpdated).Hours()
	if hoursSinceLastActivity < 1 {
		// Nothing more to be done!
		activityDetection.Detected = true
	} else {
		// Since there was no activity since the last 24 hours,
		// the Actor Identification algorithm kicks in, which
		// determines the change blockers and dependant actors
		// who should take appropriate actions to facilitate the
		// movement of the pull requests.
		activityDetection.Detected = false
	}

	return activityDetection
}

func (activity *Activity) findDelayedPRs(repo repository.RepoModel) chan []prp.PRModel {
	delayedPRs := make(chan []prp.PRModel)
	go func() {
		pr := prp.Init(activity.db)
		prList := make([]prp.PRModel, 0)
		openPRs, prErr := pr.GetOpenPRs(repo.RepoId)
		if prErr != nil {
			activity.lo.Printf("Failed to fetch open PRs for %s - %v", repo.Name, prErr)
		}

		for _, openPR := range *openPRs {
			activityCheck := activity.checkForActivity(openPR)
			if activityCheck.Detected {
				// Terminating the workflow since there is an activity observed in the last 24 hours
				continue
			} else {
				prList = append(prList, openPR)
			}
		}
		delayedPRs <- prList
	}()

	return delayedPRs
}
