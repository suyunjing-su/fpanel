package com.admin.config;


import com.admin.common.utils.JwtUtil;
import com.admin.entity.Node;
import com.admin.service.NodeService;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import lombok.extern.slf4j.Slf4j;
import org.springframework.context.annotation.Configuration;
import org.springframework.http.server.ServerHttpRequest;
import org.springframework.http.server.ServerHttpResponse;
import org.springframework.http.server.ServletServerHttpRequest;
import org.springframework.util.StringUtils;
import org.springframework.web.socket.WebSocketHandler;
import org.springframework.web.socket.server.support.HttpSessionHandshakeInterceptor;

import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;
import java.net.InetSocketAddress;
import java.util.Map;
import java.util.Objects;


@Configuration
@Slf4j
public class WebSocketInterceptor extends HttpSessionHandshakeInterceptor {

    @Resource
    NodeService nodeService;

    @Override
    public void afterHandshake(ServerHttpRequest request, ServerHttpResponse response, WebSocketHandler wsHandler, Exception ex) {

    }

    @Override
    public boolean beforeHandshake(ServerHttpRequest request, ServerHttpResponse response, WebSocketHandler wsHandler, Map<String, Object> attributes) throws Exception {
        ServletServerHttpRequest serverHttpRequest = (ServletServerHttpRequest) request;
        HttpServletRequest servletRequest = serverHttpRequest.getServletRequest();
        String type = serverHttpRequest.getServletRequest().getParameter("type");
        String version = serverHttpRequest.getServletRequest().getParameter("version");
        String http = serverHttpRequest.getServletRequest().getParameter("http");
        String tls = serverHttpRequest.getServletRequest().getParameter("tls");
        String socks = serverHttpRequest.getServletRequest().getParameter("socks");

        if (!isSecureTransport(servletRequest)) {
            log.warn("拒绝非安全WebSocket握手，IP: {}", getClientIp(request));
            return false;
        }

        if (Objects.equals(type, "1")) {
            String secret = extractBearerToken(servletRequest.getHeader("Authorization"));
            if (!StringUtils.hasText(secret)) {
                log.info("节点验证失败：缺少Authorization令牌，IP: {}", getClientIp(request));
                return false;
            }

            Node node = nodeService.getOne(new QueryWrapper<Node>().eq("secret", secret));
            if (node == null) {
                log.info("节点验证失败：未找到匹配的secret");
                return false;
            }
            attributes.put("id", node.getId());
            attributes.put("nodeSecret", secret);
            attributes.put("nodeVersion", version);
            attributes.put("http",http);
            attributes.put("tls",tls);
            attributes.put("socks",socks);
            log.info("节点 {} 通过验证，版本: {}", node.getId(), version);
            // 不在这里更新状态，等到连接建立后再统一更新
        }else {
            String secret = serverHttpRequest.getServletRequest().getParameter("secret");
            boolean b = JwtUtil.validateToken(secret);
            if (!b) return false;
            attributes.put("id", JwtUtil.getUserIdFromToken(secret));
        }
        attributes.put("type", type);
        return true;
    }

    private String extractBearerToken(String authorization) {
        if (!StringUtils.hasText(authorization)) {
            return null;
        }
        String value = authorization.trim();
        if (value.regionMatches(true, 0, "Bearer ", 0, 7)) {
            value = value.substring(7).trim();
        }
        return StringUtils.hasText(value) ? value : null;
    }

    private boolean isSecureTransport(HttpServletRequest request) {
        if (request.isSecure()) {
            return true;
        }

        String forwardedProto = request.getHeader("X-Forwarded-Proto");
        if (StringUtils.hasText(forwardedProto)) {
            String[] protocols = forwardedProto.split(",");
            for (String proto : protocols) {
                String normalized = proto == null ? "" : proto.trim().toLowerCase();
                if ("https".equals(normalized) || "wss".equals(normalized)) {
                    return true;
                }
            }
        }

        String scheme = request.getScheme();
        return "https".equalsIgnoreCase(scheme) || "wss".equalsIgnoreCase(scheme);
    }

    public String getClientIp(ServerHttpRequest request) {
        InetSocketAddress remoteAddress = request.getRemoteAddress();
        if (remoteAddress != null) {
            return remoteAddress.getAddress().getHostAddress();
        }
        return null;
    }


}
