package repository

import (
	"fmt"
	"strings"

	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/db"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/entity"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/utils"
	"github.com/Netcracker/qubership-apihub-backend/qubership-apihub-service/view"
)

type ActivityTrackingRepository interface {
	CreateEvent(ent *entity.ActivityTrackingEntity) error

	GetEvents(scope view.PackageReadScope, req view.ActivityHistoryReq, userId string) ([]entity.EnrichedActivityTrackingEntity, error)
	GetEventsForPackages(scope view.PackageReadScope, req view.ActivityHistoryReq, packageIds []string) ([]entity.EnrichedActivityTrackingEntity, error)
}

func NewActivityTrackingRepository(cp db.ConnectionProvider) ActivityTrackingRepository {
	return &activityTrackingRepositoryImpl{
		cp: cp,
	}
}

type activityTrackingRepositoryImpl struct {
	cp db.ConnectionProvider
}

func (a activityTrackingRepositoryImpl) CreateEvent(ent *entity.ActivityTrackingEntity) error {
	_, err := a.cp.GetConnection().Model(ent).Insert()
	if err != nil {
		return err
	}
	return nil
}

func (a activityTrackingRepositoryImpl) GetEvents(scope view.PackageReadScope, req view.ActivityHistoryReq, userId string) ([]entity.EnrichedActivityTrackingEntity, error) {
	var result []entity.EnrichedActivityTrackingEntity

	query, params, err := buildActivityEventsQuery(scope, req, userId, nil)
	if err != nil {
		return nil, err
	}

	if _, err := a.cp.GetConnection().Model(&params).Query(&result, query); err != nil {
		return nil, err
	}
	return result, nil
}

func (a activityTrackingRepositoryImpl) GetEventsForPackages(scope view.PackageReadScope, req view.ActivityHistoryReq, packageIds []string) ([]entity.EnrichedActivityTrackingEntity, error) {
	var result []entity.EnrichedActivityTrackingEntity

	query, params, err := buildActivityEventsQuery(scope, req, "", packageIds)
	if err != nil {
		return nil, err
	}

	if _, err := a.cp.GetConnection().Model(&params).Query(&result, query); err != nil {
		return nil, err
	}
	return result, nil
}

func buildActivityEventsQuery(scope view.PackageReadScope, req view.ActivityHistoryReq, userId string, packageIds []string) (string, entity.ActivityEventsQuery, error) {
	params := entity.ActivityEventsQuery{
		UserId:      userId,
		SubtreeRoot: scope.SubtreeRoot,
		PackageIds:  packageIds,
		Kinds:       req.Kind,
		Types:       req.Types,
		Limit:       req.Limit,
		Offset:      req.Limit * req.Page,
	}

	// The feed only needs to know whether the caller may read, not which permissions it holds.
	ctes, condition, err := renderReadScope(scope, userId, "at.package_id", "pkg")
	if err != nil {
		return "", params, err
	}

	conditions := make([]string, 0)
	if condition != "" {
		conditions = append(conditions, condition)
	}

	if req.OnlyFavorite {
		// A favourited package covers its descendants, so events below it count as favourited too.
		conditions = append(conditions, fmt.Sprintf(`exists (
		select 1 from favorite_packages fav
		where fav.user_id = ?user_id and %s
	)`, utils.SubtreeCondition("at.package_id", "fav.package_id")))
	}
	if req.OnlyShared {
		conditions = append(conditions, `exists (
		select 1 from package_member_role mem
		where mem.user_id = ?user_id and mem.package_id = at.package_id
	)`)
	}
	if len(req.Kind) > 0 {
		conditions = append(conditions, "pkg.kind = any(?kinds::text[])")
	}
	if len(packageIds) > 0 {
		conditions = append(conditions, "at.package_id = any(?package_ids::text[])")
	}
	if len(req.Types) > 0 {
		conditions = append(conditions, "at.e_type = any(?types::text[])")
	}
	if req.TextFilter != "" {
		params.TextFilter = "%" + utils.LikeEscaped(req.TextFilter) + "%"
		conditions = append(conditions, "(pkg.name ilike ?text_filter or usr.name ilike ?text_filter)")
	}

	var sb strings.Builder
	sb.WriteString(utils.WithClause(ctes))
	eventsSelect := `
	select at.*,
		get_latest_revision(at.package_id, at.data #>> '{version}') != (at.data #>> '{revision}')::int as not_latest_revision,
		pkg.name as pkg_name, pkg.kind as pkg_kind,
		usr.name as prl_usr_name, usr.email as prl_usr_email, usr.avatar_url as prl_usr_avatar_url,
		apikey.id as prl_apikey_id, apikey.name as prl_apikey_name,
		case when coalesce(usr.name, apikey.name) is null then at.user_id else usr.user_id end prl_usr_id
	from activity_tracking as at
	inner join package_group as pkg on at.package_id = pkg.id
	left join user_data as usr on at.user_id = usr.user_id
	left join apihub_api_keys as apikey on at.user_id = apikey.id`
	sb.WriteString(eventsSelect)
	if len(conditions) > 0 {
		sb.WriteString("\n\twhere ")
		sb.WriteString(strings.Join(conditions, "\n\t  and "))
	}
	//at.id breaks ties on equal dates so that paging cannot repeat or skip an event
	sb.WriteString("\n\torder by at.date desc, at.id desc")
	sb.WriteString(utils.PagingClause(params.Limit))

	return sb.String(), params, nil
}

// renderReadScope renders scope as a WHERE condition over row. The result references the named parameters
// ?user_id and ?subtree_root, so the query's parameter struct has to declare both.
func renderReadScope(scope view.PackageReadScope, userId string, idColumn string, alias string) ([]string, string, error) {
	switch scope.Kind {
	case view.PackageReadScopeAll:
		return nil, "", nil
	case view.PackageReadScopeSubtree:
		if scope.SubtreeRoot == "" {
			return nil, "", fmt.Errorf("package read scope of kind %v has no subtree root", scope.Kind)
		}
		return nil, utils.SubtreeCondition(idColumn, "?subtree_root"), nil
	case view.PackageReadScopeUser:
		if userId == "" {
			return nil, "", fmt.Errorf("package read scope of kind %v has no user id", scope.Kind)
		}
		if alias == "" {
			return nil, "", fmt.Errorf("package read scope of kind %v has no package row to read default_role from", scope.Kind)
		}
		readGrantingRolesCTE := `read_granting_roles as materialized (
			select coalesce(array_agg(id), array[]::character varying[]) as ids
			from role
			where coalesce('read' = any(permissions), false)
		)`
		userCondition := fmt.Sprintf(`case
			when %[1]s.default_role = any((select ids from read_granting_roles)::character varying[]) then true
			else exists (
				select 1
				from package_group anc
				where anc.id = any(package_ancestor_ids(%[1]s.id))
				  and anc.id <> %[1]s.id
				  and anc.default_role = any((select ids from read_granting_roles)::character varying[])
			)
			or exists (
				select 1
				from package_member_role m
				where m.package_id = any(package_ancestor_ids(%[1]s.id))
				  and m.user_id = ?user_id
				  and m.roles && (select ids from read_granting_roles)::character varying[]
			)
		end`, alias)
		return []string{readGrantingRolesCTE}, userCondition, nil
	default:
		return nil, "", fmt.Errorf("unsupported package read scope kind %v", scope.Kind)
	}
}
