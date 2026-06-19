terraform {
  required_providers {
    newrelic = {
      source  = "newrelic/newrelic"
      version = "~> 3.55"
    }
  }
}

variable "newrelic_account_id" {
  type = number
}

variable "public_locations" {
  type    = list(string)
  default = ["US_EAST_1", "US_WEST_1", "EU_WEST_1"]
}

resource "newrelic_synthetics_monitor" "dashboard_healthz" {
  account_id        = var.newrelic_account_id
  name              = "ItsBagelBot dashboard healthz"
  type              = "SIMPLE"
  status            = "ENABLED"
  period            = "EVERY_MINUTE"
  uri               = "https://dashboard.itsbagelbot.com/healthz"
  locations_public  = var.public_locations
  validation_string = "ok"
  verify_ssl        = true
}

resource "newrelic_synthetics_monitor" "dashboard_login" {
  account_id       = var.newrelic_account_id
  name             = "ItsBagelBot dashboard login route"
  type             = "SIMPLE"
  status           = "ENABLED"
  period           = "EVERY_5_MINUTES"
  uri              = "https://dashboard.itsbagelbot.com/login"
  locations_public = var.public_locations
  verify_ssl       = true
}
