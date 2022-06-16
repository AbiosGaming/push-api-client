# push-api-client [deprecated, end of life @ 20220921]
A demo websocket client showing how to subscribe to Abios' data pushes using either v3 or v2 authentication credentials. Documentation for the Abios push API can be found [in the Abios developer hub](https://abiosgaming.com/docs/en/push-api/introduction/overview).

## Requirements
You need to have valid Abios v3 or v2 API keys to run this demo client. If you don't have any keys, please contact us at `info@abiosgaming.com` and we'll help you to get setup.
 
The push api test client has been tested with Golang 1.15.x, it might work with older versions but no guarantees.

## Compiling
To compile the client:

`$ go build .`

Now you should have a binary called `push-api-client`.


## Running

See [the subscription documentation](https://abiosgaming.com/docs/en/push-api/introduction/overview#message-envelope) for information about how to write subscription specifications.

### With Atlas/v3 API authentication secret 

To start the client do:

 `$ ./push-api-client --secret=$CLIENT_SECRET --subscription-file=sample_subscription_v3.json`

where `CLIENT_SECRET` is the same that you already use to access v3 of the Abios REST API . The `sample_subscription_v3.json` file contains a simple subscription specification that will listen to all events from the `series_updates` channel (for the games your account has access to).

### With v2 API authentication client id/secret 

 `$ ./push-api-client --client-id=$CLIENT_ID --client-secret=$CLIENT_SECRET --subscription-file=sample_subscription_v2.json`

 where `CLIENT_ID` and `CLIENT_SECRET` are the same that you already use to access the Abios v2 REST API. The `sample_subscription_v2.json` file contains a simple subscription specification that will listen to all events from the `series` channel (for the games your account has access to).
