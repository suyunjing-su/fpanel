package com.admin.common.task;

import com.admin.entity.ChainTunnel;
import com.admin.service.ChainTunnelService;
import com.admin.service.NodeService;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.core.conditions.update.UpdateWrapper;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.test.util.ReflectionTestUtils;

import java.util.Collection;
import java.util.List;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.ArgumentMatchers.isNull;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoInteractions;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class ExitHealthCheckAsyncTest {

    @Mock
    private ChainTunnelService chainTunnelService;

    @Mock
    private NodeService nodeService;

    private ExitHealthCheckAsync task;

    @BeforeEach
    void setUp() {
        task = new ExitHealthCheckAsync();
        ReflectionTestUtils.setField(task, "chainTunnelService", chainTunnelService);
        ReflectionTestUtils.setField(task, "nodeService", nodeService);
    }

    @Test
    void shouldSkipObservationForSingleExitTunnel() {
        ChainTunnel singleExit = chainTunnel(1L, 100L, 200L, 3, 1, 10L);
        when(chainTunnelService.list(any(QueryWrapper.class))).thenReturn(List.of(singleExit));

        task.checkExitHealth();

        verify(chainTunnelService, times(1)).list(any(QueryWrapper.class));
        verify(chainTunnelService, never()).update(isNull(), any(UpdateWrapper.class));
        verifyNoInteractions(nodeService);
    }

    @Test
    void shouldNotPullConfigWhenSelectableStateUnchanged() {
        ChainTunnel exitA = chainTunnel(1L, 100L, 201L, 3, 0, null);
        ChainTunnel exitB = chainTunnel(2L, 100L, 202L, 3, 0, null);
        when(chainTunnelService.list(any(QueryWrapper.class))).thenReturn(List.of(exitA, exitB));
        when(nodeService.getById(anyLong())).thenReturn(null);

        task.checkExitHealth();

        verify(chainTunnelService, times(1)).list(any(QueryWrapper.class));
        verify(chainTunnelService, times(2)).update(isNull(), any(UpdateWrapper.class));
        verify(nodeService, times(2)).getById(anyLong());
    }

    @Test
    void shouldQueryEntryNodesWhenSelectableStateChanges() {
        ChainTunnel exitA = chainTunnel(1L, 100L, 201L, 3, 1, 10L);
        ChainTunnel exitB = chainTunnel(2L, 100L, 202L, 3, 0, null);
        ChainTunnel entryA = chainTunnel(3L, 100L, 301L, 1, 1, null);
        ChainTunnel entryB = chainTunnel(4L, 100L, 302L, 1, 1, null);

        when(chainTunnelService.list(any(QueryWrapper.class))).thenReturn(
                List.of(exitA, exitB),
                List.of(entryA, entryB)
        );
        when(nodeService.getById(anyLong())).thenReturn(null);

        task.checkExitHealth();

        verify(chainTunnelService, times(2)).list(any(QueryWrapper.class));
        verify(chainTunnelService, times(2)).update(isNull(), any(UpdateWrapper.class));

        ArgumentCaptor<QueryWrapper<ChainTunnel>> queryCaptor = ArgumentCaptor.forClass(QueryWrapper.class);
        verify(chainTunnelService, times(2)).list(queryCaptor.capture());
        QueryWrapper<ChainTunnel> entryQuery = queryCaptor.getAllValues().get(1);
        Map<String, Object> params = entryQuery.getParamNameValuePairs();

        assertTrue(containsValue(params.values(), 1), "entry query should constrain chain_type=1");
        assertTrue(containsValue(params.values(), 100L), "entry query should constrain changed tunnel id");
    }

    private boolean containsValue(Collection<Object> values, Object expected) {
        for (Object value : values) {
            if (expected.equals(value)) {
                return true;
            }
            if (value instanceof Collection<?> nested && nested.contains(expected)) {
                return true;
            }
        }
        return false;
    }

    private ChainTunnel chainTunnel(Long id,
                                    Long tunnelId,
                                    Long nodeId,
                                    Integer chainType,
                                    Integer healthStatus,
                                    Long latencyMs) {
        ChainTunnel chainTunnel = new ChainTunnel();
        chainTunnel.setId(id);
        chainTunnel.setTunnelId(tunnelId);
        chainTunnel.setNodeId(nodeId);
        chainTunnel.setChainType(chainType);
        chainTunnel.setHealthStatus(healthStatus);
        chainTunnel.setLastLatencyMs(latencyMs);
        return chainTunnel;
    }
}
