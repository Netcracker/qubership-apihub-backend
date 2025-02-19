

# Authentification
Apihub provides no anonymous access except some dedicated endpoints like shared document or it's own openapi spec.
Apihub supports SSO and local(disabled in production mode) authentication.

SSO is implemented via SAML and tested with ADFS.

## SSO via Saml

![SSO auth flow](./sso_flow.png)

Notes:
* Apihub is using external authentication, but issuing it's own Bearer token which is used for all requests to Apihub.
* Attributes "User-Principal-Name", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress" are mandatory in SAML response.
* Attributes "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname", "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname", "thumbnailPhoto" are not mandatory, but expected in SAML response to fill the user profile.


## Internal(local) user management
Apihub supports local user management, but this functionality is disabled in production.

TODO: description

## Api keys
TODO

## Personal Access Tokens
TODO

# Authorization
First of all let's define some terms used in the following description.
'Workspace' is a top grouping entity which may contain groups, packages, dashboards.
'Group' is a grouping entity for a list of packages.
'Package' is a representation of a service with API.
'Dashboard' is representation for a set of particulate versions of services, i.e. it's like deployment.

Apihub have built-in authorization model which is based on granted roles for workspace/group/package/dashboard.


User have a set of roles for particular entity.
Role have a set of permissions.
Roles are defined system-wide by system administrators.
Roles have an hierarchy which limits privilege escalation.

Workspace/group/package/dashboard have a default role that is assigned to a user which have no granted role.

TODO



