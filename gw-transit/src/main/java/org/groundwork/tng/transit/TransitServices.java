package org.groundwork.tng.transit;

import org.groundwork.rs.dto.DtoOperationResults;
import org.groundwork.rs.transit.DtoMetricDescriptor;
import org.groundwork.rs.transit.DtoInventory;
import org.groundwork.rs.transit.DtoResourceWithMetricsList;

import java.util.List;

public interface TransitServices {

    void SendResourcesWithMetrics(DtoResourceWithMetricsList resources) throws TransitException;

    List<DtoMetricDescriptor> ListMetrics() throws TransitException;

    void SynchronizeInventory(DtoInventory inventory) throws TransitException;

    void Disconnect() throws TransitException;
}