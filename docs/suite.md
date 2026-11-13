# Optional Comfyware connections

Each Comfyware app keeps its own accounts, sessions and data. These connections
are optional and off by default. Set them in your service configuration or
Compose `.env` file, then restart the service.

## Apps menu

Set `TIDINGS_APPS` to a JSON list of app IDs and addresses to show an **Apps**
menu. Tidings leaves itself out of the menu.

```sh
TIDINGS_APPS='[{"id":"imvault","url":"https://photos.example.org"},{"id":"songstead","url":"https://music.example.org"},{"id":"witmoot","url":"https://discuss.example.org"},{"id":"folkweave","url":"https://people.example.org"},{"id":"tidings","url":"https://read.example.org"}]'
```

Startup rejects unknown IDs, duplicates, and addresses with credentials, queries
or fragments. The menu only links; it gives nobody an account in another app,
and Tidings never contacts these addresses.

## Community sign-in

Community sign-in lets members sign in with an OpenID Connect provider:

```sh
TIDINGS_SHARED_ISSUER=https://identity.example.org
TIDINGS_SHARED_CLIENT_ID=tidings
TIDINGS_SHARED_CLIENT_SECRET=configure-in-your-protected-service-environment
```

Set `TIDINGS_BASE_URL` too, and register exactly
`https://your-app.example.org/auth/shared/callback` with the provider, using a
separate client for each app. The issuer must use HTTPS, except on loopback for
testing. Protect the client secret.

To link an identity, a member signs in with their password, opens **Account →
Community sign-in** and connects. After that, the sign-in page offers it. An
identity that is not linked cannot create an account or gain permissions.
Suspended accounts cannot sign in. Disconnecting leaves the password working.
Signing out of Tidings does not sign you out of other apps or the provider.
Keep a working owner password for provider outages.

Links are stored in the Tidings database and included in backups. Sign-ins in
progress expire after five minutes and are lost when the server restarts.

## Reading layout

The **Comfortable** and **Compact** buttons above the story list set story
spacing. The choice is saved in this browser and shared between its tabs.
