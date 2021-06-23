package newerpol

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/viper"
)

type AccessRecord struct {
	AccessId      int
	WebsiteId     int
	RequestStatus int
	FirstName     string
	LookupName    string
	Login         string
	Email         string
	CSP           string
}

type GetGrantsOptions struct {
	IncludeNonPending bool
}

// These are the statuses from dbo.WebserverAccessStatii
const (
	AccessGrantPending  = 1
	AccessGranted       = 2
	AccessRevokePending = 3
	AccessRevoked       = 4
)

// The grants lookup query ignores rows where a newer record exists for a
// given person and website so old revocations don't clobber new grants when
// non-pending grants / revocations are included in the sync
const grantsLookupQuery = `SELECT dbo.WebserverAccess.ID AS accessid,
	dbo.WebserverAccess.WebsiteId AS websiteid,
	dbo.WebserverAccess.RequestStatus AS requeststatus,
	dbo.PeopleLookup.FName AS firstname,
	dbo.PeopleLookup.LookupName AS lookupname,
	dbo.PeopleLookup.Login AS login,
	ISNULL(dbo.PeopleLookup.PrimaryEmail, '') AS email,
	dbo.AllCentres.Committee AS csp
	FROM dbo.WebserverAccess
	INNER JOIN dbo.Websites ON dbo.WebserverAccess.WebsiteID = dbo.Websites.ID
	INNER JOIN dbo.AllCentres ON dbo.Websites.OCID = dbo.AllCentres.OCID
	INNER JOIN dbo.PeopleLookup ON dbo.WebserverAccess.PeopleId = dbo.PeopleLookup.ID
	WHERE dbo.WebserverAccess.RequestStatus IN (?)
	AND Login IS NOT NULL
	AND NOT EXISTS (
		SELECT 1
		FROM WebserverAccess newer
		WHERE newer.PeopleID = dbo.WebserverAccess.PeopleID
		AND newer.WebsiteID = dbo.WebserverAccess.WebsiteID
		AND newer.SubmittedWhen > dbo.WebserverAccess.SubmittedWhen
	)`

const grantPendingToGrantedQuery = `UPDATE dbo.WebserverAccess SET RequestStatus = 2,
	GrantedWhen = GETDATE()
	WHERE dbo.WebserverAccess.ID = ?
	AND dbo.WebserverAccess.RequestStatus = ?`

const revokePendingToRevokedQuery = `UPDATE dbo.WebserverAccess SET RequestStatus = 4,
	RevokedWhen = GETDATE()
	WHERE dbo.WebserverAccess.ID = ?
	AND dbo.WebserverAccess.RequestStatus = ?`

const managedSitesLookupQuery = `SELECT dbo.Websites.ID AS id
	FROM dbo.Websites
	WHERE Deleted = 0`

var grantPendingToGrantedQueryPrepared *sql.Stmt
var revokePendingToRevokedQueryPrepared *sql.Stmt

// Connect to the Newerpol database using the Newerpol connection settings
// from configuration
func Connect() (*sqlx.DB, error) {
	query := url.Values{}
	query.Add("database", viper.GetString("newerpol.database"))

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(viper.GetString("newerpol.username"), viper.GetString("newerpol.password")),
		Host:     viper.GetString("newerpol.host"),
		Path:     viper.GetString("newerpol.instance"),
		RawQuery: query.Encode(),
	}

	return sqlx.Connect("sqlserver", u.String())
}

// Get grants to add
func GetGrantsToAdd(db *sqlx.DB, opts *GetGrantsOptions) (map[int][]AccessRecord, error) {
	accessRecordsByWebsite := make(map[int][]AccessRecord)

	states := []int{AccessGrantPending}
	if opts.IncludeNonPending {
		states = append(states, AccessGranted)
	}
	query, args, err := sqlx.In(grantsLookupQuery, states)
	if err != nil {
		return nil, fmt.Errorf("newerpol: Performing grantsLookupQuery IN subsitution: %v", err)
	}
	rows, err := db.Queryx(db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("newerpol: Performing grantsLookupQuery: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var grant AccessRecord
		if err = rows.StructScan(&grant); err != nil {
			return nil, err
		}
		accessRecordsByWebsite[grant.WebsiteId] = append(accessRecordsByWebsite[grant.WebsiteId], grant)
	}

	return accessRecordsByWebsite, nil
}

// Get grants to remove
func GetGrantsToRevoke(db *sqlx.DB, opts *GetGrantsOptions) (map[int][]AccessRecord, error) {
	accessRecordsByWebsite := make(map[int][]AccessRecord)

	states := []int{AccessRevokePending}
	if opts.IncludeNonPending {
		states = append(states, AccessRevoked)
	}
	query, args, err := sqlx.In(grantsLookupQuery, states)
	if err != nil {
		return nil, fmt.Errorf("newerpol: Performing grantsLookupQuery IN subsitution: %v", err)
	}
	rows, err := db.Queryx(db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("newerpol: Performing grantsLookupQuery: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var grant AccessRecord
		if err = rows.StructScan(&grant); err != nil {
			return nil, err
		}
		accessRecordsByWebsite[grant.WebsiteId] = append(accessRecordsByWebsite[grant.WebsiteId], grant)
	}

	return accessRecordsByWebsite, nil
}

// Get IDs of all sites managed in eActivities
func GetManagedSiteIds(db *sqlx.DB) ([]int, error) {
	var siteIds []int

	if err := db.Select(&siteIds, managedSitesLookupQuery); err != nil {
		return nil, fmt.Errorf("newerpol: Performing managedSitesLookupQuery: %v", err)
	}

	return siteIds, nil
}

func (a *AccessRecord) IsPending() bool {
	return a.RequestStatus == AccessGrantPending || a.RequestStatus == AccessRevokePending
}

// Move a grant from a pending state to a done state. Returns whether the grant updated and any error
func (a *AccessRecord) FinishGrant(db *sqlx.DB) (bool, error) {
	if a.RequestStatus == AccessGranted || a.RequestStatus == AccessRevoked {
		return false, fmt.Errorf("newerpol: Cannot finish grant, already in finished state: %+v", a)
	}

	var stmt *sql.Stmt
	var err error

	if a.RequestStatus == AccessGrantPending {
		if grantPendingToGrantedQueryPrepared == nil {
			grantPendingToGrantedQueryPrepared, err = db.Prepare(db.Rebind(grantPendingToGrantedQuery))
			if err != nil {
				return false, fmt.Errorf("newerpol: Preparing grantPendingToGrantedQuery: %v", err)
			}
		}
		stmt = grantPendingToGrantedQueryPrepared
	} else {
		if revokePendingToRevokedQueryPrepared == nil {
			revokePendingToRevokedQueryPrepared, err = db.Prepare(db.Rebind(revokePendingToRevokedQuery))
			if err != nil {
				return false, fmt.Errorf("newerpol: Preparing revokePendingToRevokedQuery: %v", err)
			}
		}
		stmt = revokePendingToRevokedQueryPrepared
	}

	result, err := stmt.Exec(a.AccessId, a.RequestStatus)
	if err != nil {
		return false, fmt.Errorf("newerpol: Finishing grant %+v: %v", a, err)
	}

	if ra, _ := result.RowsAffected(); ra == 0 {
		return false, nil
	}
	return true, nil
}
