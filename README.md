[forks-shield]: https://img.shields.io/github/forks/AdamOusmer/ItsBagelBot.svg?style=for-the-badge

[forks-url]: https://github.com/AdamOusmer/ItsBagelBot/network/members

[stars-shield]: https://img.shields.io/github/stars/AdamOusmer/ItsBagelBot.svg?style=for-the-badge

[stars-url]: https://github.com/AdamOusmer/ItsBagelBot/stargazers

[issues-shield]: https://img.shields.io/github/issues/AdamOusmer/ItsBagelBot.svg?style=for-the-badge

[issues-url]: https://github.com/AdamOusmer/ItsBagelBot/issues

[license-shield]: https://img.shields.io/badge/License-Proprietary-red.svg?style=for-the-badge

[license-url]: LICENSE.md


<!-- PROJECT LOGO -->
<div align="center">

[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![Personal][license-shield]][license-url]
[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/Q8P121QNHK)

  <a href="https://github.com/AdamOusmer/ItsBagelBot">
    <img src=".github/assets/logo.png" alt="Logo" width="200" height="200">
  </a>

<h3 align="center">ItsBagelBot</h3>

  <p align="center">
    Zero downtime, Infinite bagels.
    <br />
    Because a monolith wasn't complicated enough.
    <br />
    <br />
    <a href="https://docs.itsbagelbot.com"><strong>Explore the docs »</strong></a>
    <br />
    <a href="https://github.com/AdamOusmer/ItsBagelBot/issues/new?labels=bug&template=bug-report---.md">Report Bug</a>
    &middot;
    <a href="https://github.com/AdamOusmer/ItsBagelBot/issues/new?labels=enhancement&template=feature-request---.md">Request Feature</a>
    <br />
    <br />
    </p>

[![CodeScene Hotspot Code Health](https://codescene.io/projects/73601/status-badges/hotspot-code-health)](https://codescene.io/projects/73601)
[![CodeScene Average Code Health](https://codescene.io/projects/73601/status-badges/average-code-health)](https://codescene.io/projects/73601)
[![CodeScene System Mastery](https://codescene.io/projects/73601/status-badges/system-mastery)](https://codescene.io/projects/73601)

<br />

[![Email](https://img.shields.io/badge/contact%40itsbagelbot.com-D14836?style=for-the-badge&logo=gmail&logoColor=white)](mailto:contact@itsbagelbot.com)
[![GitHub](https://img.shields.io/badge/AdamOusmer-%23121011.svg?style=for-the-badge&logo=github&logoColor=white)](https://github.com/AdamOusmer)


</div>

***

<!-- TABLE OF CONTENTS -->
<details>
  <summary>Table of Contents</summary>
  <ol>
    <li><a href="#about-the-project">About The Project</a></li>
    <li><a href="#architecture-overview">Architecture Overview</a>
        <ul>
            <li><a href="#tech-stack">Tech Stack</a></li>
        </ul>
        <ul>
            <li><a href="#security">Security</a></li>
        </ul>
    </li>
    <li><a href="#contributors">Contributors</a></li>
    <li><a href="#contributing">Contributing</a></li>
    <li><a href="#license">License</a></li>
    <li><a href="#contact">Contact</a></li>
    <li><a href="#acknowledgements">Acknowledgements</a></li>
  </ol>
</details>

***

## About The Project

There are thousands of Twitch bots out there, yet none that quite fit my needs. ItsBagelBot is my attempt at creating a
bot that is.

I have been running so many bots at once to get the features I want, that I decided to make my own bot that can handle
everything I need in one place. ItsBagelBot is designed to be modular, so I can easily add or remove features as needed.

After years of research on making my stream better, I have finally decided to share my creation with the world.
ItsBagelBot is the culmination of all my knowledge and experience in the Twitch community.
All this in a single cloud-native, zero-downtime, microservices-based Twitch bot.

Some might say it's over-engineered for a Twitch bot. It is.

The reason? Because I can.

And because I want to learn more and apply modern software engineering practices to a fun project while showcasing my
capabilities.

The entirety of the bot is hosted on Oracle Cloud Infrastructure in a Canadian region. The location was chosen for higher
availability of the resources I need, as well as the advantages of data sovereignty and Canadian privacy laws. Moreover, the 
data centers are located in a region where hydroelectric power is abundant, making it an environmentally conscious choice.

***

## Architecture Overview

ItsBagelBot is built using a microservices architecture, with each feature being its own service. This allows for
zero-downtime updates, as services can be updated independently without affecting the entire system.

The main flow of the bot is as follows:
- **Ingress Service** (Elixir/OTP): Handles incoming Twitch events and routes them to the appropriate service.
It is based on Twitch's Conduit architecture for scalability and reliability. Twitch's EventSub WebSocket connections are managed here, normalized, filtered, and published to the message bus.
- **Message Bus**: NATS is the only inter-service transport, carrying both subject-based events (pub/sub) and request-reply RPC. No service reads another service's database directly.
- **Services** (Go): Each feature of the bot is implemented as a separate service that owns its own MySQL schema, subscribes to relevant subjects, and answers RPC for cross-service reads.
- **Outgress Service** (Go): Sends messages back to Twitch based on the processed data from the services, applying per-broadcaster rate limiting and the bot account token lifecycle.
- **Web Interface** (Astro/Bun): A modern frontend for user interaction and bot management.


### Tech Stack

Currently in development, but will be built with the following technologies:

#### Languages

![Go](https://img.shields.io/badge/go-%2300ADD8.svg?style=for-the-badge&logo=go&logoColor=white)
![Elixir](https://img.shields.io/badge/elixir-%234B275F.svg?style=for-the-badge&logo=elixir&logoColor=white)
![TypeScript](https://img.shields.io/badge/typescript-%23007ACC.svg?style=for-the-badge&logo=typescript&logoColor=white)

#### Technologies & Tools

![Astro](https://img.shields.io/badge/astro-%232C2052.svg?style=for-the-badge&logo=astro&logoColor=white)
![Bun](https://img.shields.io/badge/Bun-%23000000.svg?style=for-the-badge&logo=bun&logoColor=white)
![NATS](https://img.shields.io/badge/NATS-%2327AAE1.svg?style=for-the-badge&logo=natsdotio&logoColor=white)
![Ent](https://img.shields.io/badge/ent-%235164E3.svg?style=for-the-badge&logo=go&logoColor=white)
![MySQL](https://img.shields.io/badge/mysql-%234479A1.svg?style=for-the-badge&logo=mysql&logoColor=white)
![Valkey](https://img.shields.io/badge/valkey-%23DC382D.svg?style=for-the-badge&logo=valkey&logoColor=white)

#### Security & Encryption

![Tink](https://img.shields.io/badge/Tink-4285F4?style=for-the-badge&logo=google&logoColor=white)
![Linkerd](https://img.shields.io/badge/Linkerd-111111?style=for-the-badge&logo=linkerd&logoColor=white)
![Doppler](https://img.shields.io/badge/Doppler-%233426A4.svg?style=for-the-badge&logo=doppler&logoColor=white)

#### DevOps

![Podman](https://img.shields.io/badge/podman-%23892CA0.svg?style=for-the-badge&logo=podman&logoColor=white)
![k3s](https://img.shields.io/badge/k3s-%23FFC61C.svg?style=for-the-badge&logo=k3s&logoColor=black)
![Flux](https://img.shields.io/badge/flux-%235468FF.svg?style=for-the-badge&logo=flux&logoColor=white)
![GitHub Actions](https://img.shields.io/badge/github%20actions-%232671E5.svg?style=for-the-badge&logo=githubactions&logoColor=white)

#### Infrastructure

![Oracle](https://img.shields.io/badge/Oracle-F80000?style=for-the-badge&logo=oracle&logoColor=white)
![Cloudflare](https://img.shields.io/badge/Cloudflare-F38020?style=for-the-badge&logo=cloudflare&logoColor=white)
![Tailscale](https://img.shields.io/badge/Tailscale-121212?style=for-the-badge&logo=tailscale&logoColor=white)

#### Monitoring & Logging

![Zap](https://img.shields.io/badge/Zap-000000?style=for-the-badge&logo=uber&logoColor=white)
![New Relic](https://img.shields.io/badge/newrelic-%2300b1cc.svg?style=for-the-badge&logo=newrelic&logoColor=white)


### Security

I take security seriously. ItsBagelBot uses Tink for encryption of sensitive data at rest and in transit. To ensure that I are not using the insecurecleartextkeyset function of Tink, I have acquired a Vault (KMS) from Oracle to secure my keysets. 

The internal services communicate using a service mesh (Linkerd) to ensure secure communication between services and the NATS message bus.

The Oracle VPS hosting the bot is secured using Tailscale VPN, ensuring that only authorized devices can access the services.
The VPS is completely locked down with a strict firewall allowing only necessary ports. A VCN will eventually be used for
further isolation and avoiding Kubernetes public exposure.

Cloudflare is used for DNS management and DDoS protection.

***

## Contributors

This project exists thanks to the people who contribute.

<a href="https://github.com/AdamOusmer/ItsBagelBot/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=AdamOusmer/ItsBagelBot" />
</a>

***

## Contributing

If you have suggestions for how ItsBagelBot could be improved, or want to report a bug, please open an issue! I'd love
to hear your ideas and help you fix any problems.

For contributing code, please contact me directly at [contact@adam-ousmer.dev](mailto:contact@adam-ousmer.dev) before making
any changes or submitting a pull request.

***

## License

This project is licensed under the Proprietary License Agreement - see the [LICENSE](LICENSE.md) file for details.

***

## Contact

Adam Ousmer - [GitHub](https://github.com/adam-ousmer.dev) - [Email](mailto:contact@adam-ousmer.dev)

***

## Acknowledgements

README template inspired by [othneildrew/Best-README-Template](https://github.com/othneildrew/Best-README-Template)

 
 
