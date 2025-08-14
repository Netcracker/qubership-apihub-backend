# Local Backend development

This instruction tells how to set up local development.  

Backend could be started from IDE/cmd.

DB and UI components in docker are required to run full APIHUB application (except the agent functionality).
There's no way for start agent in docker since k8s API is required.

## Prerequisites

### Software installation

Install necessary software if it was not installed earlie. For more info please see [Newcomer environment setup](../newcomer_env_setup.md)

### DB

Run corresponding docker-compose file from /docker-compose/DB folder.
It will start Postgres DB in docker container with predefined credentials and database. So it's ready to connect from APIHUB BE.
User `apihub` with password `apihub` and database `apihub` will be created automatically.
If you use DBeaver you need to connect to PostgreSQL DB first using parameters:
```
Host=localhost
Username=postgres
Password=postgres
Port=5432
```
Don't forget to check 'Show all databases' to see all DBs.

* To create a corresponding docker image you need to issue a command:

```bash
docker-compose -f docs/local_development/docker-compose/DB/docker-compose.yml up
```

If you have another docker image (usually another DB container from another project) which could intersect with this one then you need to change PostgreSQL port settings and image port mapping in  [`DB/docker-compose.yml`](/docs/local_development/docker-compose/DB/docker-compose.yml). 
Please add two arguments into **command** section ("\-p" and "\<new port number\>") and update port mapping in the **ports** section. Default port number for PostgreSQL is **5432**.

* To run the image please issue a command below:

```bash
docker-compose -f docs/local_development/docker-compose/DB/docker-compose.yml run postgres
```

Of course, you can perform the actions above with your favorite IDE (Podman Desktop or Rancher Desktop for example).

Expected result - you will have a PostgreSQL instance running and waiting for a clients. If you do not please try to remove images, restart Docker (Podman Desktop or Rancher Desktop) and try again. If the application is unable to reach PostgreSQL then change you port settings, re-create image and try again.

## Running backends

### Apihub

Apihub backend is a product implementation which should be opensource-ready.

#### Generate private key

Apihub contains built-in identity provider and it requires RSA private key as a base secret.

Run [`generate_jwt_pkey.sh`](generate_jwt_pkey.sh), it will generate file jwt_private_key in the current directory. Paste the value to Apihub BE environment. Please mind that the key must be non-empty.

#### APIHUB BE configuration

Backend configuration is implemented via a configuration file(config.yaml), for the full configuration please refer to [the template file](../../qubership-apihub-service/config.template.yaml).

Use the `APIHUB_CONFIG_FOLDER` environment variable to specify the path to the configuration file, default value `.`.

The following configuration are required to start Apihub application:

```YAML
database:
  host: 'localhost'
  port: 5432
  name: 'apihub'
  username: 'apihub'
  password: 'apihub'
security:
  productionMode: false
  jwt:
    privateKey: '{use generated key here}'
  apihubExternalUrl: 'http://localhost:8081'
zeroDayConfiguration:
  accessToken: '<put_your_key_here - any random string, minimum 30 characters>'
  adminEmail: '<admin_login, example: apihub@mail.com>'
  adminPassword: '<admin_password, example: password>'
technicalParameters:
  listenAddress: '127.0.0.1:8090'
```

#### Run API hub

You can simply run Service.go from apihub-service project or you can try to use [`Dockerfile`](/Dockerfile) at your choice. If you will try to use Dockerfile you have to know about the proper image URL which you need to change in the file.

### Post-setup

Since you will run non-production environment you do not have any valid identity instead of internal. You need to perform the actions below to configure internal user in the newly created environment:

* create local user via `POST /api/internal/users`
* add admin role via `POST /api/internal/users/{userId}/systemRole`
* get local user token via  `POST /api/v2/auth/local`

You can use any of test tools approved by company to send REST API requests. The best request collection can be found in the [`qubership-apihub-postman-collections repository`](https://github.com/Netcracker/qubership-apihub-postman-collections). And the command above, collection and environment for local development are also included.

You can use Postman or Bruno app to run REST API requests.

### Custom service

Custom service is an Apihub extension with custom logic.

### Create m2m token

Create m2m admin token via POST `/api/v2/packages/*/apiKeys`
Asterisk means that the token will work for any package

### Envs

```INI
LISTEN_ADDRESS=127.0.0.1:8091;
DB_TYPE=postgres;
APIHUB_POSTGRESQL_HOST=localhost;
APIHUB_POSTGRESQL_PORT=5432;
NC_APIHUB_POSTGRESQL_DB_NAME=apihub_custom_service;
NC_APIHUB_POSTGRESQL_USERNAME=apihub;
NC_APIHUB_POSTGRESQL_PASSWORD=apihub;
APIHUB_URL=http://127.0.0.1:8090;
APIHUB_ACCESS_TOKEN={use generated token value here};
```

## UI


### Run UI service

Run corresponding docker-compose file from `/docker-compose/UI` folder.
It will start FE container providing you a kind of GUI on localhost:8081 that will connect to Apihub BE on :8090 and Custom service on :8091.

To create a corresponding Docker image you need to issue a command:

```bash
docker compose -f docs/local_development/docker-compose/UI/docker-compose.yml up
```

If default port (8080) was already taken by another application or Docker image you have configure another one in the **ports** section an re-create image with the command above. See [`FE/docker-compose.yml`](/docs/local_development/docker-compose/FE/docker-compose.yml)

To run the image please issue a command below:

```bash
docker compose -f docs/local_development/docker-compose/UI/docker-compose.yml run qubership-apihub-ui
```

Of course, you can perform the actions above with your favorite IDE.

### How to open web view

#### Create user
First you need to create a local user.
Open NC-newman-desktop or Bruno app and run `POST /api/internal/users` at APIHUB_HOST=localhost:8090 with body:

`{
"email":"test@mail.com",
"password":"test"
}`

#### Open web view
Go to [http://localhost:8081](http://localhost:8081) (use other port if you change it) and enter created above credentials.

#### Fill DB with data if needed
You can fill DB with data:
* download appropriate backup archive from FTP
* extract downloaded archive
* use Restore tool of you favorite application, dBeaver for example with next parameters: format: Directory, Backup file: <path to folder with extracted DB>, Discard object owner = true.