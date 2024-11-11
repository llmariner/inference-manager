import yaml
import sys


def filter_dependencies(conf, relative_paths, deps_names):
    new_deps = []
    for dep in conf['dependencies']:
        if dep['name'] in relative_paths:
            dep['repository'] = relative_paths[dep['name']]
            new_deps.append(dep)
        elif dep['name'] in deps_names:
            new_deps.append(dep)

    return {
        'apiVersion': 'v2',
        'name': 'llmariner',
        'version': '0.1.0',
        'dependencies': new_deps
    }


if __name__ == "__main__":
    # path to the Chart.yaml
    chart_yaml_path = sys.argv[1]
    # relative path to the deployments directory from the Chart.yaml
    deployments_dir = sys.argv[2]

    relative_paths = {
        'inference-manager-server': f'file://{deployments_dir}/server/',
        'inference-manager-engine': f'file://{deployments_dir}/engine/',
    }
    deps_names = [
        'model-manager-loader',
        'model-manager-server',
    ]

    with open(chart_yaml_path, 'r') as f:
        conf = yaml.safe_load(f)

    new_conf = filter_dependencies(conf, relative_paths, deps_names)

    with open(chart_yaml_path, 'w') as f:
        yaml.dump(new_conf, f)
