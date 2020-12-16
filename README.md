# push-api-client
A demo websocket client showing how to subscribe to Abios' data pushes using v3 authentication credentials. Documentation for the Abios push API can be found [on the Abios developer hub](https://abiosgaming.com/docs/en/push-api/introduction/overview).

If you use v2 of the Abios REST API you should look at the [v2 repository](https://github.com/AbiosGaming/push-api-client) instead.

## Requirements
You need to have valid Abios v3 API keys to run this demo client. If you don't have any keys, please contact us at `info@abiosgaming.com` and we'll help you to get setup.
 
The push api test client has been tested with Golang 1.15.x, it might work with older versions but no guarantees.

## Compiling
To compile the client:

`$ go build .`

Now you should have a binary called `push-api-client`.


## Running
To start the client do:

 `$ ./push-api-client --secret=$CLIENT_SECRET --subscription-file=sample_subscription.json`

where `CLIENT_SECRET` is the same that you already use to access v3 of the abios REST API . The `sample_subscription.json` file contains a simple subscription specification that will listen to all events from the `series_updates` channel (for the games your account has access to). See [the subscription documentation](https://abiosgaming.com/docs/en/push-api/introduction/overview#message-envelope) for information about how to write subscription specifications.