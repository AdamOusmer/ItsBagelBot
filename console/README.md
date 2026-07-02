[forks-shield]: https://img.shields.io/github/forks/AdamOusmer/ItsBagelBot.svg?style=for-the-badge

[forks-url]: https://github.com/AdamOusmer/ItsBagelBot/network/members

[stars-shield]: https://img.shields.io/github/stars/AdamOusmer/ItsBagelBot.svg?style=for-the-badge

[stars-url]: https://github.com/AdamOusmer/ItsBagelBot/stargazers

[issues-shield]: https://img.shields.io/github/issues/AdamOusmer/ItsBagelBot.svg?style=for-the-badge

[issues-url]: https://github.com/AdamOusmer/ItsBagelBot/issues

[license-shield]: https://img.shields.io/badge/License-Proprietary-red.svg?style=for-the-badge

[license-url]: ../LICENSE.md


<!-- PROJECT LOGO -->
<div align="center">

[![Forks][forks-shield]][forks-url]
[![Stargazers][stars-shield]][stars-url]
[![Issues][issues-shield]][issues-url]
[![Personal][license-shield]][license-url]
[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/Q8P121QNHK)

  <a href="https://github.com/AdamOusmer/ItsBagelBot">
    <img src="../.github/assets/logo.png" alt="Logo" width="200" height="200">
  </a>

<h3 align="center">ItsBagelBot - Console</h3>

  <p align="center">
    Contains the SvelteKit dashboard, admin app, and shared UI/server code.
    <br />
    Because a monolith wasn't complicated enough.
    <br />
    <br />
    <a href="https://github.com/AdamOusmer/ItsBagelBot"><strong>Explore the docs »</strong></a>
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

[![Email](https://img.shields.io/badge/contact%40adam--ousmer.dev-D14836?style=for-the-badge&logo=gmail&logoColor=white)](mailto:contact@adam-ousmer.dev)
[![GitHub](https://img.shields.io/badge/AdamOusmer-%23121011.svg?style=for-the-badge&logo=github&logoColor=white)](https://github.com/AdamOusmer)


</div>

***

<!-- TABLE OF CONTENTS -->
<details>
  <summary>Table of Contents</summary>
  <ol>
    <li><a href="#about-the-project">About The Project</a></li>
    <li><a href="#what-the-app-does">What the App Does</a></li>
    <li><a href="#development">Development</a></li>
    <li><a href="#documentation">Documentation</a></li>
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

The Console application is a part of the ItsBagelBot ecosystem, designed to handle specific responsibilities within the architecture. It is designed to be modular, so I can easily add or remove features as needed.

After years of research on making my stream better, I have finally decided to share my creation with the world.
ItsBagelBot is the culmination of all my knowledge and experience in the Twitch community.
All this in a single cloud-native, zero-downtime, microservices-based Twitch bot.

Some might say it's over-engineered for a Twitch bot. It is.

The reason? Because I can.

And because I want to learn more and apply modern software engineering practices to a fun project while showcasing my
capabilities.

The entirety of the bot is hosted on Oracle Cloud Infrastructure's in Canadian region. The location was chosen for higher
availability of the resources I need, as well as the advantages of data sovereignty and Canadian privacy laws. Moreover, the 
data centers are located in a region where hydroelectric power is abundant, making it an environmentally conscious choice.

***

## What the App Does

Console is an integral part of the multi-tenant Twitch automation platform, specifically focusing on:

- Contains the SvelteKit dashboard, admin app, and shared UI/server code.

The project is under active development. It is currently operated as a complete cloud deployment rather than distributed as a turnkey, single-container bot.

***

## Development

The whole production topology is intentionally not reproduced by a single local command. Most work can be verified at the service or package level, while integration work requires the relevant infrastructure and environment variables.

Some integration tests detect optional environment variables such as `NATS_URL` or `VALKEY_TEST_ADDR` and skip when their dependency is unavailable. Never commit credentials: production secrets are injected at runtime rather than stored in the repository.

***

## Documentation

The detailed documentation lives in [`docs/`](../docs/). Useful starting points include:

- [Current system state](../docs/src/content/docs/reference/system-overview.md) — the authoritative running shape, data plane, and request flow.
- [Architecture overview](../docs/src/content/docs/architecture/index.md) — system context and external dependencies.
- [Service registry](../docs/src/content/docs/microservices/index.md) — service ownership and communication boundaries.
- [RPC contracts](../docs/src/content/docs/reference/rpc-contracts.md) — the NATS request-reply surface.
- [Architecture decisions](../docs/src/content/docs/adr/index.md) — why the major technical choices were made.

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

This project is licensed under the Proprietary License Agreement - see the [LICENSE](../LICENSE.md) file for details.

***

## Contact

Adam Ousmer - [GitHub](https://github.com/AdamOusmer) - [Email](mailto:contact@adam-ousmer.dev)

***

## Acknowledgements

README template inspired by [othneildrew/Best-README-Template](https://github.com/othneildrew/Best-README-Template)
