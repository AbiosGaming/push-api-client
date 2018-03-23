# push-api-client
A demo websocket client showing how to subscribe to Abios' data pushes. Documentation for the Abios push API can be found [here](https://docs.abiosgaming.com/v2/reference#new-push-api-overview).

## Requirements
You need to have valid Abios API keys to run this demo client. If you don't have any keys, please contact us at `info@abiosgaming.com` and we'll help you to get setup.
 
The push api test client has been tested with Golang 1.9.x, it might work with older compiler versions.

All external library dependencies are included in the `vendor` directory. If you need to reinstall them for some reason, remove the `vendor` directory and regenerate it using the `glide` dependency management tool (see `https://glide.sh` for info on how to install it).

## Compiling
To compile the client:

`$ go build .`

Now you should have a binary called `push-api-client`.


If you want to reinstall the library dependencies, do:

`$ glide install`

This creates the `vendor` directory with all the dependencies.


## Running
To start the client do:

 `$ ./push-api-client --client-id=$CLIENT_ID --client-secret=$CLIENT_SECRET --subscription-file=sample_subscription.json`

where `CLIENT_ID` and `CLIENT_SECRET` are the same that you already use to access the abios HTTP API. The `sample_subscription.json` file contains a simple subscription specification that will listen to all events from the `series` and `matches` channels (for the games your account have access to).
