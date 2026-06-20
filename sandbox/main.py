"""XiaoTianQuant Python Sandbox CLI
提供指标代码执行和静态分析的命令行工具。
"""
import argparse
import json
import sys
from executor import safe_exec_with_validation
from analyzer import analyze_indicator_code_quality


def main():
    parser = argparse.ArgumentParser(description="XiaoTianQuant Sandbox CLI")
    subparsers = parser.add_subparsers(dest="command")

    # execute subcommand
    exec_parser = subparsers.add_parser("execute", help="Execute indicator code safely")
    exec_parser.add_argument("--code", required=True, help="Python code to execute")
    exec_parser.add_argument("--df-json", help="DataFrame JSON data")
    exec_parser.add_argument("--params", help="Parameters JSON")
    exec_parser.add_argument("--timeout", type=int, default=20)

    # analyze subcommand
    analyze_parser = subparsers.add_parser("analyze", help="Analyze indicator code quality")
    analyze_parser.add_argument("--code", required=True, help="Python code to analyze")

    args = parser.parse_args()

    if args.command == "execute":
        params = json.loads(args.params) if args.params else None
        df_json = json.loads(args.df_json) if args.df_json else None
        result = safe_exec_with_validation(
            code=args.code, df_json=df_json, params=params, timeout=args.timeout
        )
        print(json.dumps(result, indent=2))
    elif args.command == "analyze":
        hints = analyze_indicator_code_quality(args.code)
        print(json.dumps({"hints": hints}, indent=2))
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
