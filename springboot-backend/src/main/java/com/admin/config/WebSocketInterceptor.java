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
import java.net.InetAddress;
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
        String protocolHeader = servletRequest.getHeader("Sec-WebSocket-Protocol");
        String type = resolveHandshakeType(servletRequest, protocolHeader);
        String version = readHeaderValue(servletRequest, "X-Flux-Version");
        String http = readHeaderValue(servletRequest, "X-Flux-Http");
        String tls = readHeaderValue(servletRequest, "X-Flux-Tls");
        String socks = readHeaderValue(servletRequest, "X-Flux-Socks");

        if (!isSecureTransport(servletRequest)) {
            log.warn("拒绝非安全WebSocket握手，IP: {}", getClientIp(request));
            return false;
        }

        if (Objects.equals(type, "1")) {
            String secret = resolveWebSocketToken(servletRequest);
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
            String secret = resolveWebSocketToken(servletRequest);
            boolean b = JwtUtil.validateToken(secret);
            if (!b) return false;
            attributes.put("id", JwtUtil.getUserIdFromToken(secret));
        }
        attributes.put("type", type);
        return true;
    }

    private String resolveWebSocketToken(HttpServletRequest request) {
        String token = extractBearerToken(request.getHeader("Authorization"));
        if (StringUtils.hasText(token)) {
            return token;
        }

        return extractTokenFromWebSocketProtocol(request.getHeader("Sec-WebSocket-Protocol"));
    }

    private String extractTokenFromWebSocketProtocol(String protocolHeader) {
        if (!StringUtils.hasText(protocolHeader)) {
            return null;
        }

        String[] protocols = protocolHeader.split(",");
        if (protocols.length == 0) {
            return null;
        }

        for (int i = 0; i < protocols.length; i++) {
            String current = protocols[i] == null ? "" : protocols[i].trim();
            if (!"auth-token".equalsIgnoreCase(current) && !"bearer".equalsIgnoreCase(current)) {
                continue;
            }
            if (i + 1 >= protocols.length) {
                return null;
            }

            String token = protocols[i + 1] == null ? "" : protocols[i + 1].trim();
            return StringUtils.hasText(token) ? token : null;
        }

        return null;
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
        return true;
    }

    private boolean hasSecureProxySignal(HttpServletRequest request) {
        if (containsSecureProto(request.getHeader("X-Forwarded-Proto"))) {
            return true;
        }

        String forwarded = request.getHeader("Forwarded");
        if (StringUtils.hasText(forwarded) && forwarded.toLowerCase().contains("proto=https")) {
            return true;
        }

        String cfVisitor = request.getHeader("CF-Visitor");
        if (StringUtils.hasText(cfVisitor)) {
            String normalized = cfVisitor.toLowerCase().replace(" ", "");
            if (normalized.contains("\"scheme\":\"https\"") || normalized.contains("\"scheme\":\"wss\"")) {
                return true;
            }
        }

        String xForwardedSsl = request.getHeader("X-Forwarded-Ssl");
        if (StringUtils.hasText(xForwardedSsl) && "on".equalsIgnoreCase(xForwardedSsl.trim())) {
            return true;
        }

        String frontEndHttps = request.getHeader("Front-End-Https");
        return StringUtils.hasText(frontEndHttps) && "on".equalsIgnoreCase(frontEndHttps.trim());
    }

    private boolean containsSecureProto(String headerValue) {
        if (!StringUtils.hasText(headerValue)) {
            return false;
        }

        String[] protocols = headerValue.split(",");
        for (String proto : protocols) {
            String normalized = proto == null ? "" : proto.trim().toLowerCase();
            if ("https".equals(normalized) || "wss".equals(normalized)) {
                return true;
            }
        }
        return false;
    }

    private boolean isTrustedProxySource(HttpServletRequest request) {
        String remoteAddr = request.getRemoteAddr();
        if (!StringUtils.hasText(remoteAddr)) {
            return false;
        }
        try {
            InetAddress ip = InetAddress.getByName(remoteAddr);
            return ip.isLoopbackAddress() || ip.isSiteLocalAddress();
        } catch (Exception ignored) {
            return false;
        }
    }

    public String getClientIp(ServerHttpRequest request) {
        InetSocketAddress remoteAddress = request.getRemoteAddress();
        if (remoteAddress != null) {
            return remoteAddress.getAddress().getHostAddress();
        }
        return null;
    }

    private String readHeaderValue(HttpServletRequest request, String headerKey) {
        String headerValue = request.getHeader(headerKey);
        if (StringUtils.hasText(headerValue)) {
            return headerValue.trim();
        }

        return null;
    }

    private String resolveHandshakeType(HttpServletRequest request, String protocolHeader) {
        String type = readHeaderValue(request, "X-Flux-Type");
        if (StringUtils.hasText(type)) {
            return type;
        }

        if (!StringUtils.hasText(protocolHeader)) {
            return null;
        }

        String[] protocols = protocolHeader.split(",");
        for (String protocol : protocols) {
            String normalized = protocol == null ? "" : protocol.trim().toLowerCase();
            if (normalized.startsWith("flux-type-")) {
                return normalized.substring("flux-type-".length());
            }
        }

        return "0";
    }


}
