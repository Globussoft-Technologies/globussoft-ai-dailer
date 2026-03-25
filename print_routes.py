from main import app

routes = []
for route in app.routes:
    # Handle mounted apps
    if hasattr(route, 'methods'):
        routes.append(f"{list(route.methods)} {route.path}")
    else:
        routes.append(f"MOUNT {route.path}")

print("ROUTES:")
for r in routes:
    print(r)
