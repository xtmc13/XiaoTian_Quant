"""
MCP (Model Context Protocol) Server for XiaoTianQuant Agent Gateway.
Provides standardized tool interface for AI agents to interact with the trading system.
"""

from .server import MCPServer, create_mcp_server

__all__ = ["MCPServer", "create_mcp_server"]
