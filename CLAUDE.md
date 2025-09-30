# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

PT Nexus is a PT (Private Tracker) seed aggregation viewing platform that analyzes seed data and traffic information from qBittorrent and Transmission downloaders. It provides cross-client seed aggregation, traffic statistics, and site/group analytics.

## Architecture

1. **Frontend**: Vue 3 + TypeScript + Vite application (in `webui/` directory)
2. **Backend**: Python Flask application (in `server/` directory)
3. **Database**: SQLite (default), MySQL, or PostgreSQL support
4. **Go Proxy**: High-performance API proxy for qBittorrent with remote media processing capabilities
5. **Batch Enhancer**: Go application for batch processing tasks

## Key Components

### Backend Structure
- `app.py`: Main Flask application entry point
- `database.py`: Database management with support for SQLite/MySQL/PostgreSQL
- `config.py`: Configuration management with environment variable support
- `core/services.py`: Background data tracking and synchronization services
- `api/`: API route handlers
- `core/`: Core business logic including uploaders and extractors
- `utils/`: Utility functions for data processing

### Frontend Structure
- Vue 3 with TypeScript
- Element Plus component library
- Pinia for state management
- Vue Router for navigation

## Common Development Commands

### Backend Development
```bash
# Install Python dependencies
pip install -r server/requirements.txt

# Run the Flask application
python server/app.py

# Environment variables for configuration:
# DB_TYPE=sqlite|mysql|postgresql
# MYSQL_* or POSTGRES_* variables for database configuration
```

### Frontend Development
```bash
# Install dependencies
cd webui && pnpm install

# Development server
pnpm dev

# Build for production
pnpm build

# Type checking
pnpm type-check

# Linting
pnpm lint

# Formatting
pnpm format
```

### Docker Build Process
The Dockerfile uses a multi-stage build:
1. Build Vue frontend with Node.js
2. Build Go batch enhancer
3. Create final runtime image with Python dependencies

### Database Migrations
The application automatically handles schema migrations at startup. Database tables are created/updated in the `init_db()` function in `database.py`.

## Site Configuration
Site-specific configurations are stored in YAML files in `server/configs/` directory. These define:
- Form field mappings
- Source parameter parsers
- Standard key mappings
- Parameter mappings for site-specific values

## API Structure
- `/api/torrents/*`: Torrent data and management
- `/api/stats/*`: Traffic statistics
- `/api/sites/*`: Site configuration
- `/api/auth/*`: Authentication
- `/api/migrate/*`: Migration utilities
- `/api/management/*`: System management

## Proxy Services
The Go proxy service provides:
- `/api/torrents/all`: Concurrent seed list retrieval
- `/api/stats/server`: Server statistics
- `/api/media/screenshot`: Remote screenshot processing
- `/api/media/mediainfo`: Remote MediaInfo processing
- `/api/health`: Health checks

## Testing
Tests can be run with standard Python testing frameworks. For frontend, use:
```bash
# Run a single test
pnpm test:unit --testNamePattern="specific test name"

# Run all tests
pnpm test:unit
```