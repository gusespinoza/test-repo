import { DatePipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { EMPTY } from 'rxjs';
import { catchError, filter, map, switchMap, tap } from 'rxjs/operators';

import { Event } from './events.models';
import { EventsService } from './events.service';

@Component({
  selector: 'app-event-detail',
  standalone: true,
  imports: [DatePipe, RouterLink],
  templateUrl: './event-detail.component.html',
  styleUrl: './event-detail.component.css'
})
export class EventDetailComponent {
  private readonly route = inject(ActivatedRoute);
  private readonly eventsService = inject(EventsService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly event = signal<Event | null>(null);
  protected readonly loading = signal(true);
  protected readonly error = signal<string | null>(null);

  constructor() {
    this.route.paramMap
      .pipe(
        map((params) => params.get('id')),
        filter((id): id is string => id !== null),
        tap(() => {
          this.event.set(null);
          this.loading.set(true);
          this.error.set(null);
        }),
        switchMap((id) =>
          this.eventsService.getEvent(id).pipe(
            catchError((error: HttpErrorResponse) => {
              this.error.set(error.error?.error ?? 'No se pudo cargar el evento.');
              this.loading.set(false);
              return EMPTY;
            })
          )
        ),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe((event) => {
        this.event.set(event);
        this.loading.set(false);
      });
  }
}
