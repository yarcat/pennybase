# Pennybase

Poor man's Backend-as-a-Service (BaaS), similar to Firebase/Supabase/Pocketbase

It implements core backend features in less than 1000 lines of Go code, using only standard library and no external dependencies:

- **File-based storage** using CSV with versioned records
- **REST API** with JSON responses
- **Authentication** with session cookies and Basic Auth
- **RBAC & ownership-based permissions**
- **Real-time updates** via SSE
- **Schema validation** for numbers/text/lists
- **Template rendering** with Go templates

## How it Works

Data stored in human-readable CSVs, one row per record. Data storage is append-only, with each update creating a new version of the record. The latest version is always used for reads. For faster lookups and updates, Pennybase maintains an in-memory index of the latest versions (offsets from the beginning of the CSV file).

We agree that the first column in CSV is always the record ID, and the second column is the version number. The rest of the columns are data fields.

To put JSON resources into such CSV format, Pennybase uses a simple schema definition in `_schemas.csv` that maps JSON fields to CSV columns. Typically it looks like this:

```csv
s1,1,_permissions,_id,text,,,^.+$
s2,1,_permissions,_v,number,1,,
s3,1,_permissions,resource,text,,,^.+$
s4,1,_permissions,action,text,,,^.+$
s5,1,_permissions,field,text,,,^.*$
s6,1,_permissions,role,text,,,^.*$
s7,1,_users,_id,text,,,^.+$
s8,1,_users,_v,number,1,,
s9,1,_users,salt,text,,,
s10,1,_users,password,text,,,^.+$
s11,1,_users,roles,list,,,
s12,1,todo,_id,text,,,^.+$
s13,1,todo,_v,number,1,,
s14,1,todo,description,text,0,0,".+"
s15,1,todo,completed,number,0,1,""
```

Here first column is ID, second is version number (schemas are immutable), then comes the resource/collection name, followed by field name, field type, min/max value for numbers, and validation regex for strings.

For simplicity only text, number and list field type are supported.

Another important file is `_users.csv` which contains user credentials and roles. It has the same format as other resources, but with a special `_users` collection name. There is no way to add new users via API, they must be created manually by editing this file:

```csv
admin,1,salt,5V5R4SO4ZIFMXRZUL2EQMT2CJSREI7EMTK7AH2ND3T7BXIDLMNVQ====,"admin"
alice,1,salt,PXHQWNPTZCBORTO5ASIJYVVAINQLQKJSOAQ4UXIAKTR55BU4HGRQ====,
```

Here we have user ID which is user name, version number (always 1), salt for password hashing, and the password itself (hashed with SHA-256 and encoded as Base32). The last column is a list of roles assigned to the user.

One last special file is `_permissions.csv` which defines access control rules for resources. Each row defines a rule that allows access to a resource:

```csv
p1,1,todo,read,,*,"Anyone authenticated can read todos"
p2,1,todo,create,,*,"Anyone authenticated can add new todos"
p3,1,todo,update,owner,"admin,editor","Only owners and users with admin or editor role can update todos"
p4,1,todo,delete,owner,"admin,editor","Only owners and users with admin or editor role can delete todos"
```

It's very basic role-based access control: when the system needs to perform an action on a resource it checks the matching permission rule (there may be more then one). If the user has one of the roles in the list - permission is granted. Alternatively, if the resource field specified in the rule matches user ID - permission is granted as well (in the example above "owner" is the field of "todo" resource that contains owner user ID). If no rules match - access is denied.

## REST API

Based on the resources defined in `_schemas.csv`, Pennybase provides a REST API with the following endpoints:

- `GET /api/{resource}?sort_by={field}` - list all records in the resource, optionally sorting them
- `GET /api/{resource}/{id}` - get a single record by ID
- `POST /api/{resource}` - create a new record (requires "create" permission)
- `PUT /api/{resource}/{id}` - update an existing record (requires "update" permission)
- `DELETE /api/{resource}/{id}` - delete a record (requires "delete" permission)
- `GET /api/events/{resource}` - stream server-side events for a resource (requires "read" permission)

One may use basic auth to authenticate requests, or use session cookies. Session cookies are created by sending a POST request to `/api/login` with `username` and `password` fields in the body. The response will contain a session cookie that can be used for subsequent requests. Calling `/api/logout` will invalidate the session and remove the cookie.

## Static assets

Pennybase can also serve static assets from the `static` directory. You can place your HTML, CSS, JavaScript files there and access them via `/{filename}` URL.

Additionally, Pennybase supports rendering HTML templates using Go's `html/template` package. You can create a template file in the `templates` directory and access it via `/{filename}` URL as well. The following data is available in the templates:

* `.User` - the currently authenticated user (or `nil` if not authenticated)
* `.Store` - the Pennybase store instance, for reading or listing resources
* `.Request` - the current HTTP request
* `.ID` - the ID of the resource being accessed (if applicable)
* `.Authorize` - a function to check if the user has permission to perform an action on a resource:

```html
{{ if .User }}
{{ if call .Authorize "my-resource" "read" .ID }}
<div>{{ .Store.Get "my-resource" .ID }}</div>
{{ else }}
<div>You do not have permission to read this resource.</div>
{{ end }}
{{ else }}
<div>Please log in to access this resource.</div>
{{ end }}
```

## Hooks

Extending Pennybase functionality is possible via hooks. Or, technically, one hook function:

```go
server, err := pennybase.NewServer("data", "templates", "static")
if err != nil {
    log.Fatal(err)
}
server.Hook = func(trigger, resource string, user pennybase.Resource, res pennybase.Resource) error {
    log.Printf("Hook triggered: %s on %s by user %v: %v", trigger, resource, user, res)
    if trigger == "create" && resource == "messages" {
        r["author"] = user["_id"]
        r["created_at"] = time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")
    }
    return nil
}
log.Fatal(http.ListenAndServe(":8000", server))
```

This hook will be called on every create/update/delete action on any resource. The `trigger` parameter indicates the action type, `resource` is the name of the resource being modified, `user` is the user performing the action, and `res` is the resource data being modified.

You may perform additional validation or modify the resource data before it is saved. If you return an error from the hook, the action will be aborted and an error response will be sent to the client.

## Contributions

Contributions are welcome, but please make sure the code remains small, clear and correct.
Likely, no new features would be added, except for bug fixes, tests and examples.

The code is distributed under MIT license, so feel free to play with it, fork it, do anything you want!
