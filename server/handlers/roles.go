package handlers

import (
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"

	"github.com/google/uuid"
)

type createRoleRequest struct {
	Name        string `json:"name"`
	Permissions int64  `json:"permissions"`
	Color       string `json:"color"`
}

type updateRoleRequest struct {
	Name        *string `json:"name,omitempty"`
	Permissions *int64  `json:"permissions,omitempty"`
	Position    *int    `json:"position,omitempty"`
	Color       *string `json:"color,omitempty"`
}

func (d *Deps) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := d.Roles.List()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to list roles")
		return
	}
	if roles == nil {
		roles = []models.Role{}
	}
	util.JSON(w, http.StatusOK, roles)
}

type swapRolePositionsRequest struct {
	RoleID1 string `json:"role_id_1"`
	RoleID2 string `json:"role_id_2"`
}

func (d *Deps) SwapRolePositions(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageRoles); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req swapRolePositionsRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RoleID1 == "" || req.RoleID2 == "" {
		util.Error(w, http.StatusBadRequest, "role_id_1 and role_id_2 are required")
		return
	}

	role1, err := d.Roles.GetByID(req.RoleID1)
	if err != nil {
		util.Error(w, http.StatusNotFound, "role not found")
		return
	}
	role2, err := d.Roles.GetByID(req.RoleID2)
	if err != nil {
		util.Error(w, http.StatusNotFound, "role not found")
		return
	}

	// Actor nesmí přesouvat role nad svou vlastní position
	minPos := role1.Position
	if role2.Position < minPos {
		minPos = role2.Position
	}
	if err := d.canActOnRole(user, minPos); err != nil {
		util.Error(w, http.StatusForbidden, "cannot move roles above your own rank")
		return
	}

	if err := d.Roles.SwapPositions(req.RoleID1, req.RoleID2); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to swap positions")
		return
	}

	roles, _ := d.Roles.List()
	if roles == nil {
		roles = []models.Role{}
	}
	util.JSON(w, http.StatusOK, roles)
}

func (d *Deps) CreateRole(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageRoles); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req createRoleRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		util.Error(w, http.StatusBadRequest, "name is required")
		return
	}

	// Kontrola: nemůže nastavit permissions, které sám nemá
	if !user.IsOwner {
		actorPerms, err := d.Roles.GetUserPermissions(user.ID)
		if err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to check permissions")
			return
		}
		if actorPerms&models.PermAdmin == 0 && (req.Permissions & ^actorPerms) != 0 {
			util.Error(w, http.StatusForbidden, "cannot grant permissions you don't have")
			return
		}
	}

	id, _ := uuid.NewV7()
	role := &models.Role{
		ID:          id.String(),
		Name:        req.Name,
		Permissions: req.Permissions,
		Color:       req.Color,
	}

	if err := d.Roles.Create(role); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to create role")
		return
	}

	role, _ = d.Roles.GetByID(role.ID)

	d.logAudit(user.ID, "role.create", "role", role.ID, map[string]string{"name": role.Name})

	util.JSON(w, http.StatusCreated, role)
}

func (d *Deps) UpdateRole(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageRoles); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")
	role, err := d.Roles.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "role not found")
		return
	}

	// Hierarchická kontrola — nesmí upravovat role s vyšší/stejnou position
	if err := d.canActOnRole(user, role.Position); err != nil {
		util.Error(w, http.StatusForbidden, "cannot modify a role with higher or equal rank")
		return
	}

	var req updateRoleRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Kontrola: nemůže nastavit permissions, které sám nemá
	if req.Permissions != nil && !user.IsOwner {
		actorPerms, err := d.Roles.GetUserPermissions(user.ID)
		if err != nil {
			util.Error(w, http.StatusInternalServerError, "failed to check permissions")
			return
		}
		if actorPerms&models.PermAdmin == 0 && (*req.Permissions & ^actorPerms) != 0 {
			util.Error(w, http.StatusForbidden, "cannot grant permissions you don't have")
			return
		}
	}

	if req.Name != nil {
		role.Name = *req.Name
	}
	if req.Permissions != nil {
		role.Permissions = *req.Permissions
	}
	if req.Color != nil {
		role.Color = *req.Color
	}
	if req.Position != nil {
		// Kontrola: nesmí nastavit position vyšší (nižší číslo) než vlastní rank
		if !user.IsOwner {
			actorPos, _ := d.Roles.GetHighestPosition(user.ID, user.IsOwner)
			if *req.Position <= actorPos {
				util.Error(w, http.StatusForbidden, "cannot set role position above your own rank")
				return
			}
		}
		role.Position = *req.Position
	}

	if err := d.Roles.Update(role); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to update role")
		return
	}

	d.logAudit(user.ID, "role.update", "role", role.ID, map[string]string{"name": role.Name})

	util.JSON(w, http.StatusOK, role)
}

func (d *Deps) DeleteRole(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageRoles); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	id := r.PathValue("id")
	if id == "everyone" {
		util.Error(w, http.StatusBadRequest, "cannot delete everyone role")
		return
	}

	// Hierarchická kontrola
	role, err := d.Roles.GetByID(id)
	if err != nil {
		util.Error(w, http.StatusNotFound, "role not found")
		return
	}
	if err := d.canActOnRole(user, role.Position); err != nil {
		util.Error(w, http.StatusForbidden, "cannot delete a role with higher or equal rank")
		return
	}

	if err := d.Roles.Delete(id); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to delete role")
		return
	}

	d.logAudit(user.ID, "role.delete", "role", id, map[string]string{"name": role.Name})

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) GetUserRoles(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	roles, err := d.Roles.GetUserRoles(userID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to get user roles")
		return
	}
	if roles == nil {
		roles = []models.Role{}
	}
	util.JSON(w, http.StatusOK, roles)
}

func (d *Deps) AssignRole(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageRoles); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")
	roleID := r.PathValue("roleId")

	// Hierarchická kontrola — nesmí přiřazovat role s vyšší/stejnou position
	role, err := d.Roles.GetByID(roleID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "role not found")
		return
	}
	if err := d.canActOnRole(user, role.Position); err != nil {
		util.Error(w, http.StatusForbidden, "cannot assign a role with higher or equal rank")
		return
	}

	if err := d.Roles.AssignToUser(userID, roleID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to assign role")
		return
	}

	d.logAudit(user.ID, "role.assign", "role", roleID, map[string]string{"target_user_id": userID, "role_name": role.Name})

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *Deps) RemoveRole(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if err := d.requirePermission(user, models.PermManageRoles); err != nil {
		util.Error(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	userID := r.PathValue("userId")
	roleID := r.PathValue("roleId")

	// Hierarchická kontrola — nesmí odebírat role s vyšší/stejnou position
	role, err := d.Roles.GetByID(roleID)
	if err != nil {
		util.Error(w, http.StatusNotFound, "role not found")
		return
	}
	if err := d.canActOnRole(user, role.Position); err != nil {
		util.Error(w, http.StatusForbidden, "cannot remove a role with higher or equal rank")
		return
	}

	if err := d.Roles.RemoveFromUser(userID, roleID); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to remove role")
		return
	}

	d.logAudit(user.ID, "role.remove", "role", roleID, map[string]string{"target_user_id": userID, "role_name": role.Name})

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
