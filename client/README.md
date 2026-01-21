# AI DBA Workbench Client

The web client for the AI DBA Workbench application.

## Overview

The AI DBA Workbench Client is a React-based single-page application that
provides the user interface for the AI DBA Workbench. It includes a login
page, header bar, help panel, and theme switching capabilities.

## Prerequisites

- Node.js 18 or later
- npm 9 or later

## Getting Started

Install dependencies.

```bash
npm install
```

Start the development server.

```bash
npm run dev
```

The application starts at http://localhost:5173 by default.

## Building

Create a production build.

```bash
npm run build
```

The output is generated in the `dist` directory.

## Testing

Run the test suite.

```bash
npm test
```

Run tests in watch mode.

```bash
npm run test:watch
```

Generate a coverage report.

```bash
npm run test:coverage
```

## Project Structure

```
client/
  src/
    assets/        # Static assets (images, fonts)
    components/    # React components
    contexts/      # React context providers
    hooks/         # Custom React hooks
    lib/           # Utility libraries
    theme/         # MUI theme configuration
    test/          # Test utilities and setup
    App.jsx        # Main application component
    main.jsx       # Application entry point
```

## Configuration

The development server proxies API requests to the server running on
port 8080. Configure the proxy in `vite.config.js` if your server runs
on a different port.

## License

This project is licensed under the [PostgreSQL License](../LICENSE.md).
