package com.bffgin.backend.domain;

import java.time.LocalDate;
import java.util.List;

public record TaskInput(
        String name,
        String description,
        String statusRaw,
        LocalDate finishedOn,
        List<Long> labelIds) {
}
